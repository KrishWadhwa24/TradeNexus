package insights

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/cronx"
	"tradenexus/internal/market"
)

// confluenceWindowDays is how far back the confluence board looks for aligning
// bullish signals.
const confluenceWindowDays = 7

// Service computes cross-signal analytics and maintains the signal_outcomes
// snapshot table.
type Service struct {
	pool   *pgxpool.Pool
	minNet float64 // ₹ net-value threshold for a "big" bulk/block buyer
	log    zerolog.Logger
}

// New builds the insights service. minNetValue is the bulk/block net-buyer
// threshold reused for the confluence board.
func New(pool *pgxpool.Pool, minNetValue float64, log zerolog.Logger) *Service {
	if minNetValue <= 0 {
		minNetValue = 50_000_000
	}
	return &Service{pool: pool, minNet: minNetValue, log: log}
}

// StartRecorder records outcomes once at startup, then daily at 03:10 IST
// (after the overnight scan/cleanup jobs), until ctx is cancelled.
func (s *Service) StartRecorder(ctx context.Context) {
	go func() {
		rc, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.recordOutcomes(rc)
	}()

	c := cron.New(cron.WithLocation(market.IST), cron.WithChain(cronx.Recover(s.log)))
	if _, err := c.AddFunc("10 3 * * *", func() {
		rc, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.recordOutcomes(rc)
	}); err != nil {
		s.log.Error().Err(err).Msg("insights: recorder cron invalid")
		return
	}
	c.Start()
	go func() { <-ctx.Done(); c.Stop() }()
	s.log.Info().Msg("insights recorder started")
}

// outcomeRow is a stored outcome we still need to mature.
type outcomeRow struct {
	signalID     int64
	instrumentID int64
	direction    string
	candleDate   time.Time
	entryClose   float64
}

// recordOutcomes runs two passes. Pass A creates a base outcome row for each
// signal not yet recorded (while the signal still exists in the signals table,
// which is pruned at ~30 calendar days). Pass B matures every outcome whose
// longest horizon (30 trading days ≈ 42 calendar days) isn't filled yet, reading
// from the persistent signal_outcomes table + daily_candles — so maturation is
// decoupled from the signals-table retention.
func (s *Service) recordOutcomes(ctx context.Context) {
	created := s.createNewOutcomes(ctx)
	matured := s.matureOutcomes(ctx)
	if created > 0 || matured > 0 {
		s.log.Info().Int("created", created).Int("matured", matured).Msg("insights: signal outcomes updated")
	}
}

// createNewOutcomes inserts a base row (entry price only; horizons filled later
// by matureOutcomes) for signals not yet in signal_outcomes.
func (s *Service) createNewOutcomes(ctx context.Context) int {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.instrument_id, s.source, s.scanner_name, s.timeframe, s.direction, s.candle_date
		FROM signals s LEFT JOIN signal_outcomes o ON o.signal_id = s.id
		WHERE o.signal_id IS NULL`)
	if err != nil {
		s.log.Error().Err(err).Msg("insights: load new signals failed")
		return 0
	}
	type newSig struct {
		id, instID                            int64
		source, scanner, timeframe, direction string
		candleDate                            time.Time
	}
	var todo []newSig
	for rows.Next() {
		var n newSig
		if err := rows.Scan(&n.id, &n.instID, &n.source, &n.scanner, &n.timeframe, &n.direction, &n.candleDate); err != nil {
			rows.Close()
			s.log.Error().Err(err).Msg("insights: scan new signal failed")
			return 0
		}
		todo = append(todo, n)
	}
	rows.Close()

	n := 0
	for _, sg := range todo {
		entry := s.closeAt(ctx, sg.instID, sg.candleDate)
		if entry <= 0 {
			continue // signal's own candle not stored yet — retry next run
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO signal_outcomes
				(signal_id, instrument_id, source, scanner_name, timeframe, direction, candle_date, entry_close)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (signal_id) DO NOTHING`,
			sg.id, sg.instID, sg.source, sg.scanner, sg.timeframe, sg.direction, sg.candleDate, entry)
		if err != nil {
			s.log.Error().Err(err).Int64("signal", sg.id).Msg("insights: insert outcome failed")
			continue
		}
		n++
	}
	return n
}

// matureOutcomes recomputes forward returns for outcomes whose 30-day horizon
// isn't filled, from candles. Returns how many became fully matured this run.
func (s *Service) matureOutcomes(ctx context.Context) int {
	rows, err := s.pool.Query(ctx, `
		SELECT signal_id, instrument_id, direction, candle_date, entry_close
		FROM signal_outcomes WHERE ret_30d IS NULL`)
	if err != nil {
		s.log.Error().Err(err).Msg("insights: load incomplete outcomes failed")
		return 0
	}
	var todo []outcomeRow
	for rows.Next() {
		var o outcomeRow
		if err := rows.Scan(&o.signalID, &o.instrumentID, &o.direction, &o.candleDate, &o.entryClose); err != nil {
			rows.Close()
			s.log.Error().Err(err).Msg("insights: scan outcome failed")
			return 0
		}
		todo = append(todo, o)
	}
	rows.Close()

	fullyMatured := 0
	for _, o := range todo {
		if o.entryClose <= 0 {
			continue
		}
		closes, err := s.forwardCloses(ctx, o.instrumentID, o.candleDate)
		if err != nil || len(closes) == 0 {
			continue
		}
		buy := strings.EqualFold(o.direction, "BUY")
		r5 := ret(closes, 5, o.entryClose, buy)
		r10 := ret(closes, 10, o.entryClose, buy)
		r20 := ret(closes, 20, o.entryClose, buy)
		r30 := ret(closes, 30, o.entryClose, buy)
		if _, err := s.pool.Exec(ctx, `
			UPDATE signal_outcomes SET ret_5d=$2, ret_10d=$3, ret_20d=$4, ret_30d=$5, updated_at=now()
			WHERE signal_id=$1`, o.signalID, r5, r10, r20, r30); err != nil {
			s.log.Error().Err(err).Int64("signal", o.signalID).Msg("insights: update outcome failed")
			continue
		}
		if r30 != nil {
			fullyMatured++
		}
	}
	return fullyMatured
}

// closeAt returns the close on/after date for an instrument (0 if none stored).
func (s *Service) closeAt(ctx context.Context, instrumentID int64, date time.Time) float64 {
	var c float64
	_ = s.pool.QueryRow(ctx, `
		SELECT close FROM daily_candles
		WHERE instrument_id = $1 AND trade_date >= $2
		ORDER BY trade_date ASC LIMIT 1`, instrumentID, date).Scan(&c)
	return c
}

// forwardCloses returns closes from the signal's candle date forward (index 0 =
// the signal's own bar), up to 31 trading days (enough for the 30-day horizon).
func (s *Service) forwardCloses(ctx context.Context, instrumentID int64, from time.Time) ([]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT close FROM daily_candles
		WHERE instrument_id = $1 AND trade_date >= $2
		ORDER BY trade_date ASC LIMIT 31`, instrumentID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var c float64
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ret is the directional forward return % at horizon h trading days, or nil if
// that horizon hasn't matured yet.
func ret(closes []float64, h int, entry float64, buy bool) *float64 {
	if h >= len(closes) {
		return nil
	}
	r := (closes[h] - entry) / entry * 100
	if !buy {
		r = -r
	}
	return &r
}

// SignalPerformance returns forward-return stats per scanner+timeframe.
func (s *Service) SignalPerformance(ctx context.Context) ([]ScannerPerf, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT source, timeframe,
			count(*) FILTER (WHERE ret_5d  IS NOT NULL),
			coalesce(avg(ret_5d), 0),
			coalesce(avg(CASE WHEN ret_5d  > 0 THEN 1.0 ELSE 0.0 END) FILTER (WHERE ret_5d  IS NOT NULL), 0),
			count(*) FILTER (WHERE ret_10d IS NOT NULL),
			coalesce(avg(ret_10d), 0),
			coalesce(avg(CASE WHEN ret_10d > 0 THEN 1.0 ELSE 0.0 END) FILTER (WHERE ret_10d IS NOT NULL), 0),
			count(*) FILTER (WHERE ret_20d IS NOT NULL),
			coalesce(avg(ret_20d), 0),
			coalesce(avg(CASE WHEN ret_20d > 0 THEN 1.0 ELSE 0.0 END) FILTER (WHERE ret_20d IS NOT NULL), 0),
			count(*) FILTER (WHERE ret_30d IS NOT NULL),
			coalesce(avg(ret_30d), 0),
			coalesce(avg(CASE WHEN ret_30d > 0 THEN 1.0 ELSE 0.0 END) FILTER (WHERE ret_30d IS NOT NULL), 0)
		FROM signal_outcomes
		GROUP BY source, timeframe
		HAVING count(*) FILTER (WHERE ret_5d IS NOT NULL) > 0
		ORDER BY source, timeframe`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScannerPerf
	for rows.Next() {
		var p ScannerPerf
		if err := rows.Scan(&p.Source, &p.Timeframe,
			&p.D5.N, &p.D5.AvgReturn, &p.D5.WinRate,
			&p.D10.N, &p.D10.AvgReturn, &p.D10.WinRate,
			&p.D20.N, &p.D20.AvgReturn, &p.D20.WinRate,
			&p.D30.N, &p.D30.AvgReturn, &p.D30.WinRate); err != nil {
			return nil, err
		}
		p.D5.WinRate *= 100
		p.D10.WinRate *= 100
		p.D20.WinRate *= 100
		p.D30.WinRate *= 100
		p.Label = perfLabel(p.Source, p.Timeframe)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Breadth returns daily BUY vs SELL signal counts over the last `days` days.
func (s *Service) Breadth(ctx context.Context, days int) ([]BreadthPoint, error) {
	if days <= 0 {
		days = 30
	}
	from := dateOnly(time.Now().In(market.IST)).AddDate(0, 0, -days)
	rows, err := s.pool.Query(ctx, `
		SELECT candle_date,
			count(*) FILTER (WHERE direction = 'BUY'),
			count(*) FILTER (WHERE direction = 'SELL')
		FROM signals WHERE candle_date >= $1
		GROUP BY candle_date ORDER BY candle_date`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BreadthPoint
	for rows.Next() {
		var b BreadthPoint
		if err := rows.Scan(&b.Date, &b.Buys, &b.Sells); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Confluence returns stocks where 2+ independent bullish sources aligned within
// the confluence window, ranked by how many aligned.
func (s *Service) Confluence(ctx context.Context) ([]ConfluenceStock, error) {
	since := dateOnly(time.Now().In(market.IST)).AddDate(0, 0, -confluenceWindowDays)
	board := map[string]*ConfluenceStock{}
	get := func(sym, name string) *ConfluenceStock {
		c, ok := board[sym]
		if !ok {
			c = &ConfluenceStock{Symbol: sym}
			board[sym] = c
		}
		if c.Name == "" && name != "" {
			c.Name = name
		}
		return c
	}

	// 1) Scanner BUY signals (map instrument's -EQ symbol → bare NSE symbol).
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT i.trading_symbol, i.name
		FROM signals s JOIN instruments i ON i.id = s.instrument_id
		WHERE s.direction = 'BUY' AND s.candle_date >= $1`, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ts, name string
		if err := rows.Scan(&ts, &name); err != nil {
			rows.Close()
			return nil, err
		}
		get(bareSymbol(ts), name).ScannerBuy = true
	}
	rows.Close()

	// 2) Promoter/KMP buys.
	pr, err := s.pool.Query(ctx, `
		SELECT DISTINCT symbol, company_name FROM promoter_trades
		WHERE event_type LIKE '%buy' AND broadcast_at >= $1`, since)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var sym, name string
		if err := pr.Scan(&sym, &name); err != nil {
			pr.Close()
			return nil, err
		}
		get(strings.ToUpper(sym), name).PromoterBuy = true
	}
	pr.Close()

	// 3/4) Bulk & block deals with a net buyer above the ₹ threshold.
	for _, dt := range []string{"bulk", "block"} {
		dr, err := s.pool.Query(ctx, `
			SELECT symbol, max(security_name) FROM (
				SELECT symbol, security_name, client_name,
					sum(CASE WHEN buy_sell='BUY' THEN quantity*price ELSE -quantity*price END) AS net_val
				FROM market_deals
				WHERE deal_type = $1 AND deal_date >= $2
				GROUP BY symbol, security_name, client_name
			) t WHERE net_val >= $3 GROUP BY symbol`, dt, since, s.minNet)
		if err != nil {
			return nil, err
		}
		for dr.Next() {
			var sym, name string
			if err := dr.Scan(&sym, &name); err != nil {
				dr.Close()
				return nil, err
			}
			c := get(strings.ToUpper(sym), name)
			if dt == "bulk" {
				c.BulkBuy = true
			} else {
				c.BlockBuy = true
			}
		}
		dr.Close()
	}

	// Score, keep only real alignment (2+ sources), sort.
	var out []ConfluenceStock
	for _, c := range board {
		c.Sources = sourcesOf(c)
		c.Score = len(c.Sources)
		if c.Score >= 2 {
			out = append(out, *c)
		}
	}
	sortByScore(out)
	return out, nil
}
