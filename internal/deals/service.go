package deals

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/market"
)

// Broadcaster sends a plain message to a Telegram destination. The bulk and
// block services each get their own (topic-routed) broadcaster. Implemented by
// notify.BulkDealsBroadcaster / notify.BlockDealsBroadcaster.
type Broadcaster interface {
	Broadcast(ctx context.Context, text string) (int, error)
}

// publishHour/publishMinute is when NSE publishes the day's deals (18:30 IST).
// The daily alert cron runs after this; a startup catch-up only fires if we're
// already past it.
const (
	publishHour   = 18
	publishMinute = 30
	// alertTopN caps how many net buyers/sellers a single alert lists.
	bulkTopN  = 3
	blockTopN = 5
)

// alertJobTimeout bounds a full alert pass. Sends are paced (~3s/message to the
// same group) and a 429 waits out Telegram's retry_after, so a large first-run
// burst can legitimately take several minutes — this keeps it from being cut
// off mid-way while still guaranteeing the goroutine can't run forever.
const alertJobTimeout = 30 * time.Minute

// Service fetches the NSE bulk/block deals CSV feed, stores rows, and fires
// per-stock Telegram alerts (bulk: net-value filtered; block: all stocks).
type Service struct {
	client      *Client
	repo        *Repo
	bulkBC      Broadcaster // routed to the Bulk Deals topic; nil disables bulk alerts
	blockBC     Broadcaster // routed to the Block Deals topic; nil disables block alerts
	retention   int         // days of history to keep (and backfill) for the UI
	alertWindow int         // alert stocks dealt within this many days; older = silent
	minNet      float64     // bulk-deal significance threshold (₹ net value)
	alertCron   string      // IST cron for the daily fetch+alert (e.g. "0 19 * * *")
	log         zerolog.Logger
}

// New builds the deals service. bulkBC/blockBC may be nil (alerts disabled).
func New(client *Client, repo *Repo, bulkBC, blockBC Broadcaster, retentionDays, alertWindowDays int, minNetValue float64, alertCron string, log zerolog.Logger) *Service {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if alertWindowDays <= 0 {
		alertWindowDays = 7
	}
	if alertCron == "" {
		alertCron = "0 19 * * *"
	}
	return &Service{
		client: client, repo: repo, bulkBC: bulkBC, blockBC: blockBC,
		retention: retentionDays, alertWindow: alertWindowDays, minNet: minNetValue,
		alertCron: alertCron, log: log,
	}
}

// ListStocks returns the card summaries for a deal type over the retention
// window, newest activity first. Bulk stocks are filtered to those with at
// least one client clearing the net-value threshold; block stocks are all shown.
func (s *Service) ListStocks(ctx context.Context, t Type) ([]StockSummary, error) {
	rows, err := s.repo.RowsInWindow(ctx, t, s.retention)
	if err != nil {
		return nil, err
	}
	bySym := groupBySymbol(rows)
	out := make([]StockSummary, 0, len(bySym))
	for _, grp := range bySym {
		nets := netByClient(grp)
		if t == Bulk && !significant(nets, s.minNet) {
			continue
		}
		out = append(out, summarize(grp, nets))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastDealDate.Equal(out[j].LastDealDate) {
			return dealSize(out[i]) > dealSize(out[j])
		}
		return out[i].LastDealDate.After(out[j].LastDealDate)
	})
	return out, nil
}

// GetStock returns the detail (per-client nets + raw rows) for one symbol.
func (s *Service) GetStock(ctx context.Context, t Type, symbol string) (StockDetail, error) {
	rows, err := s.repo.RowsForSymbol(ctx, t, symbol, s.retention)
	if err != nil {
		return StockDetail{}, err
	}
	nets := netByClient(rows)
	buyers, sellers := splitBuyersSellers(nets)
	d := StockDetail{
		Symbol: symbol, Days: s.retention,
		NetBuyers: buyers, NetSellers: sellers, Rows: rows,
	}
	if len(rows) > 0 {
		d.SecurityName = firstSecurityName(rows)
	}
	var buyQty, sellQty int64
	for _, c := range nets {
		d.BuyValue += c.BuyValue
		d.SellValue += c.SellValue
		buyQty += c.BuyQty
		sellQty += c.SellQty
	}
	d.TradedQty = buyQty
	if sellQty > buyQty {
		d.TradedQty = sellQty
	}
	return d, nil
}

// ListAudit returns the sent-alert ledger for a deal type, enriched with each
// alert's net position (recomputed from stored rows), most recent first.
func (s *Service) ListAudit(ctx context.Context, t Type) ([]AuditEntry, error) {
	markers, err := s.repo.ListAlerted(ctx, t, s.retention)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.RowsInWindow(ctx, t, s.retention)
	if err != nil {
		return nil, err
	}
	// Index rows by symbol|date so each audit entry's net is a map lookup.
	type key struct {
		sym string
		day string
	}
	grouped := make(map[key][]Row)
	for _, r := range rows {
		k := key{r.Symbol, r.Date.Format("2006-01-02")}
		grouped[k] = append(grouped[k], r)
	}
	out := make([]AuditEntry, 0, len(markers))
	for _, m := range markers {
		e := AuditEntry{Symbol: m.Symbol, DealDate: m.DealDate, AlertedAt: m.AlertedAt}
		if grp := grouped[key{m.Symbol, m.DealDate.Format("2006-01-02")}]; len(grp) > 0 {
			sum := summarize(grp, netByClient(grp))
			e.SecurityName = sum.SecurityName
			e.BuyValue, e.SellValue, e.TradedQty = sum.BuyValue, sum.SellValue, sum.TradedQty
		}
		out = append(out, e)
	}
	return out, nil
}

// Poll fetches [from, to] for both deal types and stores them (no alerting).
// Used for the startup backfill.
func (s *Service) Poll(ctx context.Context, from, to time.Time) error {
	for _, t := range []Type{Bulk, Block} {
		rows, err := s.client.Fetch(ctx, t, from, to)
		if err != nil {
			s.log.Error().Err(err).Str("type", string(t)).Msg("deals: fetch failed")
			continue
		}
		n, err := s.repo.InsertRows(ctx, rows)
		if err != nil {
			s.log.Error().Err(err).Str("type", string(t)).Msg("deals: store failed")
			continue
		}
		s.log.Info().Str("type", string(t)).Int("fetched", len(rows)).Int("new", n).Msg("deals: backfill stored")
	}
	return nil
}

// RefreshNow triggers an immediate fetch+store+alert pass for both deal
// types — the same work the daily alert cron performs. Used by the manual
// "refresh feed" admin action.
func (s *Service) RefreshNow(ctx context.Context) {
	s.ProcessRecent(ctx)
}

// ProcessRecent fetches the alert window for both deal types, stores the rows,
// and fires one alert per not-yet-alerted (stock, day) whose deal date is
// within the alert window. Idempotent via the ledger, so the daily cron and the
// startup catch-up can both call it safely: in steady state only the freshly
// published day is new, but a missed day (server was down) still self-heals as
// long as it's within the window. Days older than the window are never alerted
// (stored silently, shown on the website only).
func (s *Service) ProcessRecent(ctx context.Context) {
	now := time.Now().In(market.IST)
	from := now.AddDate(0, 0, -s.alertWindow)
	for _, t := range []Type{Bulk, Block} {
		rows, err := s.client.Fetch(ctx, t, from, now)
		if err != nil {
			s.log.Error().Err(err).Str("type", string(t)).Msg("deals: alert fetch failed")
			continue
		}
		if _, err := s.repo.InsertRows(ctx, rows); err != nil {
			s.log.Error().Err(err).Str("type", string(t)).Msg("deals: alert store failed")
			continue
		}
		s.alertWindowStocks(ctx, t)
	}
}

// alertWindowStocks sends one alert per qualifying, not-yet-alerted (stock, day)
// within the alert window. Groups are processed oldest-day-first so alerts go
// out in chronological order.
func (s *Service) alertWindowStocks(ctx context.Context, t Type) {
	bc := s.bulkBC
	topN := bulkTopN
	if t == Block {
		bc, topN = s.blockBC, blockTopN
	}
	if bc == nil {
		return
	}
	rows, err := s.repo.RowsInWindow(ctx, t, s.alertWindow)
	if err != nil {
		s.log.Error().Err(err).Str("type", string(t)).Msg("deals: load window for alert failed")
		return
	}

	// Group by (day, symbol) — each day's deals in a stock are one alert.
	type alertGroup struct {
		symbol string
		day    time.Time
		rows   []Row
		nets   []ClientNet
		size   float64 // deal size (larger of buy/sell side) for ordering
	}
	byKey := make(map[string]*alertGroup)
	for _, r := range rows {
		key := r.Date.Format("2006-01-02") + "|" + r.Symbol
		g, ok := byKey[key]
		if !ok {
			g = &alertGroup{symbol: r.Symbol, day: r.Date}
			byKey[key] = g
		}
		g.rows = append(g.rows, r)
	}
	groups := make([]*alertGroup, 0, len(byKey))
	for _, g := range byKey {
		g.nets = netByClient(g.rows)
		g.size = dealSize(summarize(g.rows, g.nets))
		groups = append(groups, g)
	}
	// Oldest day first, then biggest deal first within a day. In steady state
	// there's only one day (today), so alerts simply go out biggest-first; on a
	// multi-day catch-up (server was down >1 day) days replay chronologically
	// oldest→newest, each day's deals ordered biggest-first.
	sort.Slice(groups, func(i, j int) bool {
		if !groups[i].day.Equal(groups[j].day) {
			return groups[i].day.Before(groups[j].day)
		}
		return groups[i].size > groups[j].size
	})

	sent := 0
	for _, g := range groups {
		if t == Bulk && !significant(g.nets, s.minNet) {
			continue // churn-only bulk stock — skip
		}
		done, err := s.repo.AlreadyAlerted(ctx, t, g.day, g.symbol)
		if err != nil {
			s.log.Error().Err(err).Msg("deals: alert-ledger check failed")
			continue
		}
		if done {
			continue
		}
		if _, err := bc.Broadcast(ctx, formatDealMessage(t, g.rows, g.nets, g.day, topN)); err != nil {
			s.log.Error().Err(err).Str("symbol", g.symbol).Msg("deals: alert broadcast failed")
			continue // no ledger row → retried next run
		}
		if err := s.repo.MarkAlerted(ctx, t, g.day, g.symbol); err != nil {
			s.log.Error().Err(err).Msg("deals: mark alerted failed")
		}
		sent++
	}
	if sent > 0 {
		s.log.Info().Str("type", string(t)).Int("alerts", sent).Msg("deals: alerts sent")
	}
}

// SendAlert force-sends one stock's alert for the most recent stored day,
// ignoring the alert ledger. Backs the admin "send alert" test action.
func (s *Service) SendAlert(ctx context.Context, t Type, symbol string) error {
	bc := s.bulkBC
	topN := bulkTopN
	if t == Block {
		bc, topN = s.blockBC, blockTopN
	}
	if bc == nil {
		return fmt.Errorf("notifications disabled")
	}
	rows, err := s.repo.RowsForSymbol(ctx, t, symbol, s.retention)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no %s deals found for %s", t, symbol)
	}
	day := rows[0].Date // newest first
	dayRows := filterByDate(rows, day)
	_, err = bc.Broadcast(ctx, formatDealMessage(t, dayRows, netByClient(dayRows), day, topN))
	return err
}

// StartPolling runs the startup backfill, an opportunistic catch-up for today
// (if we're already past NSE's publish time), then a daily fetch+alert cron and
// a daily retention prune, until ctx is cancelled.
func (s *Service) StartPolling(ctx context.Context) {
	go func() {
		now := time.Now().In(market.IST)
		bc, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := s.Poll(bc, now.AddDate(0, 0, -s.retention), now); err != nil {
			s.log.Error().Err(err).Msg("deals: startup backfill failed")
		}
		cancel()
		// Alert on recent days (ledger-gated, so a restart won't re-send). This
		// is Telegram-rate-limited/paced, so a large first-run burst can take a
		// while — give it a generous timeout of its own (see alertJobTimeout).
		pc, pcancel := context.WithTimeout(context.Background(), alertJobTimeout)
		defer pcancel()
		s.ProcessRecent(pc)
	}()

	// Evening store-only refresh: NSE occasionally trickles in late rows after
	// the 18:30 publish. This keeps the website current between the daily alert
	// pass and midnight WITHOUT alerting — late additions for an already-alerted
	// stock show on the site only (per product decision).
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				now := time.Now().In(market.IST)
				if !afterPublish(now) { // only in the post-publish evening window
					continue
				}
				rc, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				_ = s.Poll(rc, now, now)
				cancel()
			}
		}
	}()

	c := cron.New(cron.WithLocation(market.IST))
	if _, err := c.AddFunc(s.alertCron, func() {
		pc, cancel := context.WithTimeout(context.Background(), alertJobTimeout)
		defer cancel()
		s.ProcessRecent(pc)
	}); err != nil {
		s.log.Error().Err(err).Str("cron", s.alertCron).Msg("deals: alert cron invalid")
	}
	// Daily retention prune at 02:40 IST.
	if _, err := c.AddFunc("40 2 * * *", func() {
		pc, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cutoff := time.Now().In(market.IST).AddDate(0, 0, -s.retention)
		if n, err := s.repo.PruneOlderThan(pc, cutoff); err != nil {
			s.log.Error().Err(err).Msg("deals: prune failed")
		} else if n > 0 {
			s.log.Info().Int64("removed", n).Msg("deals: pruned old rows")
		}
	}); err != nil {
		s.log.Error().Err(err).Msg("deals: prune cron invalid")
	}
	c.Start()
	go func() { <-ctx.Done(); c.Stop() }()

	s.log.Info().Str("alert_cron", s.alertCron).Int("retention_days", s.retention).
		Float64("bulk_min_net", s.minNet).Msg("deals tracker started")
}

// ---- helpers ------------------------------------------------------------

func groupBySymbol(rows []Row) map[string][]Row {
	m := make(map[string][]Row)
	for _, r := range rows {
		m[r.Symbol] = append(m[r.Symbol], r)
	}
	return m
}

func filterByDate(rows []Row, day time.Time) []Row {
	var out []Row
	for _, r := range rows {
		if sameDate(r.Date, day) {
			out = append(out, r)
		}
	}
	return out
}

func firstSecurityName(rows []Row) string {
	for _, r := range rows {
		if r.SecurityName != "" {
			return r.SecurityName
		}
	}
	return ""
}

// summarize builds a StockSummary from a symbol's rows + precomputed nets.
func summarize(rows []Row, nets []ClientNet) StockSummary {
	s := StockSummary{Symbol: rows[0].Symbol, SecurityName: firstSecurityName(rows)}
	for _, r := range rows {
		if r.Date.After(s.LastDealDate) {
			s.LastDealDate = r.Date
		}
	}
	var buyQty, sellQty int64
	for _, c := range nets {
		s.BuyValue += c.BuyValue
		s.SellValue += c.SellValue
		buyQty += c.BuyQty
		sellQty += c.SellQty
		if c.NetQty > 0 {
			s.BuyerCount++
		} else if c.NetQty < 0 {
			s.SellerCount++
		}
		if absValue(c) > absF(s.TopNetValue) {
			s.TopNetValue, s.TopNetQty, s.TopNetClient = c.NetValue, c.NetQty, c.ClientName
		}
	}
	s.TradedQty = buyQty
	if sellQty > buyQty {
		s.TradedQty = sellQty
	}
	return s
}

// dealSize is the larger of the buy/sell side value — a stock's headline size.
func dealSize(s StockSummary) float64 {
	if s.SellValue > s.BuyValue {
		return s.SellValue
	}
	return s.BuyValue
}

// formatDealMessage builds the Telegram alert for one stock on one day.
func formatDealMessage(t Type, rows []Row, nets []ClientNet, day time.Time, topN int) string {
	buyers, sellers := splitBuyersSellers(nets)
	mark, label := "🟠", "BULK DEAL"
	if t == Block {
		mark, label = "🔵", "BLOCK DEAL"
	}
	var b strings.Builder
	sym := rows[0].Symbol
	name := firstSecurityName(rows)
	fmt.Fprintf(&b, "%s %s — %s", mark, label, sym)
	if name != "" {
		fmt.Fprintf(&b, " (%s)", name)
	}
	b.WriteString("\n━━━━━━━━━━━━━━━━━━━\n")

	b.WriteString("🟢 Top Buyers (net)\n")
	writeClientLines(&b, buyers, topN)
	b.WriteString("🔴 Top Sellers (net)\n")
	writeClientLines(&b, sellers, topN)

	// Buy vs sell side totals (stock-level net is ~0 for matched block deals,
	// so it's meaningless — show both sides instead).
	var buyQty, sellQty int64
	var buyVal, sellVal float64
	for _, c := range nets {
		buyQty += c.BuyQty
		sellQty += c.SellQty
		buyVal += c.BuyValue
		sellVal += c.SellValue
	}
	tradedQty := buyQty
	if sellQty > buyQty {
		tradedQty = sellQty
	}
	b.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "🟢 Bought: %s (%s sh)\n", rupees(buyVal), groupInt(buyQty))
	fmt.Fprintf(&b, "🔴 Sold: %s (%s sh)\n", rupees(sellVal), groupInt(sellQty))
	fmt.Fprintf(&b, "📦 Shares: %s\n", groupInt(tradedQty))
	fmt.Fprintf(&b, "🗓 %s", day.Format("02-Jan-2006"))
	return b.String()
}

func writeClientLines(b *strings.Builder, nets []ClientNet, topN int) {
	if len(nets) == 0 {
		b.WriteString("  —\n")
		return
	}
	if len(nets) > topN {
		nets = nets[:topN]
	}
	for i, c := range nets {
		qty := c.NetQty
		if qty < 0 {
			qty = -qty
		}
		fmt.Fprintf(b, "%d. %s — %s @ ₹%s (%s)\n",
			i+1, c.ClientName, groupInt(qty), trimNum(c.AvgPrice()), rupees(absValue(c)))
	}
}

// rupees renders a rupee amount compactly (₹1.23 Cr / ₹45.6 L / ₹1,234).
func rupees(v float64) string {
	switch {
	case v >= 1e7:
		return "₹" + trimNum(v/1e7) + " Cr"
	case v >= 1e5:
		return "₹" + trimNum(v/1e5) + " L"
	default:
		return "₹" + groupInt(int64(v))
	}
}

// groupInt formats an integer with Indian digit grouping (12,34,567).
func groupInt(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	head, tail := s[:len(s)-3], s[len(s)-3:]
	var parts []string
	for len(head) > 2 {
		parts = append([]string{head[len(head)-2:]}, parts...)
		head = head[:len(head)-2]
	}
	parts = append([]string{head}, parts...)
	out := strings.Join(parts, ",") + "," + tail
	if neg {
		out = "-" + out
	}
	return out
}

func trimNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// afterPublish reports whether now (IST) is at/after NSE's 18:30 publish time.
func afterPublish(now time.Time) bool {
	h, m := now.Hour(), now.Minute()
	return h > publishHour || (h == publishHour && m >= publishMinute)
}

func dateOnly(t time.Time) time.Time {
	t = t.In(market.IST)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)
}

func sameDate(a, b time.Time) bool {
	a, b = a.In(market.IST), b.In(market.IST)
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
