package live

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/instruments"
)

const (
	angelStreamURL = "wss://smartapisocket.angelone.in/smart-stream"

	// ModeLTP is a bare last-traded-price tick (~51 byte packet) — used for
	// every existing equity/watchlist/open-position subscription.
	ModeLTP = 1
	// ModeSnapQuote adds volume, open interest, and best-5 bid/ask depth
	// (~379 byte packet) — used for the option chain, where LTP alone can be
	// a stale print sitting outside the live bid/ask (see
	// angel.QuoteFull.EffectivePrice's doc comment for why that matters).
	ModeSnapQuote = 3

	actionUnsubscribe = 0
	actionSubscribe   = 1

	writeWait         = 5 * time.Second
	pongWait          = 70 * time.Second
	heartbeatInterval = 25 * time.Second
	reconnectInitial  = 1 * time.Second
	reconnectMax      = 30 * time.Second
)

type Tick struct {
	InstrumentID int64     `json:"instrument_id"`
	Symbol       string    `json:"symbol"`
	Exchange     string    `json:"exchange"`
	SymbolToken  string    `json:"symbol_token"`
	Price        float64   `json:"price"`
	Bid          float64   `json:"bid,omitempty"`
	Ask          float64   `json:"ask,omitempty"`
	Volume       int64     `json:"volume,omitempty"`
	OpenInterest float64   `json:"open_interest,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// clientWant is what one subscriber (one channel) asked for one instrument:
// the instrument itself plus the minimum tick fidelity it needs.
type clientWant struct {
	instrument instruments.Instrument
	mode       int
}

// subItem pairs an instrument with the mode a subscribe/unsubscribe wire
// message should use for it.
type subItem struct {
	instrument instruments.Instrument
	mode       int
}

type Hub struct {
	angel *angel.Client
	log   zerolog.Logger

	mu           sync.Mutex
	conn         *websocket.Conn
	writeMu      sync.Mutex
	clients      map[chan Tick]map[string]clientWant
	subs         map[string]*subscription
	reconnecting bool

	lastTickMu sync.RWMutex
	lastTicks  map[string]Tick
}

type subscription struct {
	instrument instruments.Instrument
	refs       int
	// mode is the highest mode any current subscriber has asked for this
	// token — never downgraded while refs remain (see SubscribeMode's doc
	// comment). Angel packets are self-tagged with the mode of the request
	// that produced them, so subscribers wanting less than this still parse
	// the richer packet fine; they just ignore the extra fields.
	mode       int
	subscribed bool
	pending    bool
}

func NewHub(angelClient *angel.Client, log zerolog.Logger) *Hub {
	return &Hub{
		angel:     angelClient,
		log:       log,
		clients:   map[chan Tick]map[string]clientWant{},
		subs:      map[string]*subscription{},
		lastTicks: map[string]Tick{},
	}
}

func (h *Hub) GetLastTick(exchange, symbolToken string) (Tick, bool) {
	h.lastTickMu.RLock()
	defer h.lastTickMu.RUnlock()
	tick, ok := h.lastTicks[key(exchange, symbolToken)]
	return tick, ok
}

// Subscribe requests bare LTP ticks — equivalent to SubscribeMode(ctx, items,
// ModeLTP). Every pre-existing caller (watchlists, open positions, the
// public landing-page feed) keeps using this and is unaffected by
// SubscribeMode's addition.
func (h *Hub) Subscribe(ctx context.Context, items []instruments.Instrument) (<-chan Tick, func(), error) {
	return h.SubscribeMode(ctx, items, ModeLTP)
}

// SubscribeMode requests ticks for items at the given mode (ModeLTP or
// ModeSnapQuote). If another subscriber already wants a token at a lower
// mode, that token's wire subscription is upgraded to the higher mode (a
// SnapQuote packet is a superset of an LTP one, so every subscriber is
// still satisfied) — see subscription.mode's doc comment for why this never
// downgrades again once refs drop.
func (h *Hub) SubscribeMode(ctx context.Context, items []instruments.Instrument, mode int) (<-chan Tick, func(), error) {
	ch := make(chan Tick, 64)
	wanted := map[string]clientWant{}
	for _, it := range items {
		if exchangeType(it.Exchange) == 0 || it.SymbolToken == "" {
			continue
		}
		wanted[key(it.Exchange, it.SymbolToken)] = clientWant{instrument: it, mode: mode}
	}
	if len(wanted) == 0 {
		close(ch)
		return ch, func() {}, nil
	}

	toSend := h.addClient(ch, wanted)

	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			h.removeClient(ch, wanted)
		})
	}

	if err := h.ensureConnected(ctx); err != nil {
		cancel()
		return nil, nil, err
	}
	if len(toSend) > 0 {
		if err := h.sendSubscription(actionSubscribe, toSend); err != nil {
			h.markSubscribeResult(toSend, false)
			cancel()
			if h.hasClients() {
				h.startReconnect()
			}
			return nil, nil, err
		}
		h.resolveFollowUp(toSend)
	}
	return ch, cancel, nil
}

func (h *Hub) addClient(ch chan Tick, wanted map[string]clientWant) []subItem {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[ch] = wanted
	var toSend []subItem
	for k, w := range wanted {
		sub, ok := h.subs[k]
		if !ok {
			sub = &subscription{instrument: w.instrument}
			h.subs[k] = sub
		}
		sub.instrument = w.instrument
		sub.refs++

		needsSend := !sub.subscribed && !sub.pending
		if w.mode > sub.mode {
			sub.mode = w.mode
			if sub.subscribed && !sub.pending {
				needsSend = true // already on the wire, but at a lower mode — upgrade it
			}
		}
		if needsSend {
			sub.pending = true
			toSend = append(toSend, subItem{instrument: w.instrument, mode: sub.mode})
		}
	}
	return toSend
}

func (h *Hub) removeClient(ch chan Tick, wanted map[string]clientWant) {
	var toUnsubscribe []subItem
	var toPrune []string

	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	for k, w := range wanted {
		sub, ok := h.subs[k]
		if !ok {
			continue
		}
		if sub.refs > 0 {
			sub.refs--
		}
		if sub.refs == 0 {
			toUnsubscribe = append(toUnsubscribe, subItem{instrument: w.instrument, mode: sub.mode})
			toPrune = append(toPrune, k)
			delete(h.subs, k)
		}
	}
	h.mu.Unlock()

	if len(toPrune) > 0 {
		// A fully-unsubscribed token's cached tick must not outlive the
		// subscription: without this, a token that leaves the option
		// chain's ATM window and later re-enters it could have
		// liveQuote/GetLastTick serve an arbitrarily stale cached price as
		// "live" the instant it's resubscribed, before any fresh packet
		// arrives (found during the Part B review of chain_live_ws.go).
		h.lastTickMu.Lock()
		for _, k := range toPrune {
			delete(h.lastTicks, k)
		}
		h.lastTickMu.Unlock()
	}

	if len(toUnsubscribe) > 0 {
		if err := h.sendSubscription(actionUnsubscribe, toUnsubscribe); err != nil {
			h.log.Debug().Err(err).Msg("angel websocket unsubscribe skipped")
		}
	}
}

// markSubscribeResult finalizes a subscribe attempt for items sent at the
// mode recorded on each subItem. Returns follow-up items to resend: if a
// concurrent SubscribeMode call raised a token's mode again while this send
// was still in flight (the new mode is only visible on sub.mode, since
// items' own mode fields are a snapshot from before the send), the wire
// subscription is now stuck at the mode we just sent even though the Hub's
// bookkeeping says higher. See resolveFollowUp.
func (h *Hub) markSubscribeResult(items []subItem, ok bool) []subItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	var followUp []subItem
	for _, si := range items {
		sub := h.subs[key(si.instrument.Exchange, si.instrument.SymbolToken)]
		if sub == nil {
			continue
		}
		sub.pending = false
		if ok {
			sub.subscribed = true
			if sub.mode > si.mode {
				sub.pending = true
				followUp = append(followUp, subItem{instrument: sub.instrument, mode: sub.mode})
			}
		}
	}
	return followUp
}

// resolveFollowUp sends one extra subscribe round if markSubscribeResult
// reports a token's mode moved again while the first send was in flight —
// closes the race where SubscribeMode(modeA) and a concurrent
// SubscribeMode(modeB > modeA) for the same brand-new token could leave the
// wire stuck at modeA (found during the Part B review). A further
// simultaneous race within this same narrow window is a known, accepted
// residual gap — it self-heals on the Hub's next reconnect, which always
// resends every active subscription at its current sub.mode from scratch.
func (h *Hub) resolveFollowUp(sent []subItem) {
	followUp := h.markSubscribeResult(sent, true)
	if len(followUp) == 0 {
		return
	}
	if err := h.sendSubscription(actionSubscribe, followUp); err != nil {
		h.markSubscribeResult(followUp, false)
		return
	}
	h.markSubscribeResult(followUp, true)
}

func (h *Hub) ensureConnected(ctx context.Context) error {
	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	conn, err := h.connect(ctx)
	if err != nil {
		return err
	}

	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	h.conn = conn
	h.mu.Unlock()

	go h.readLoop(conn)
	go h.pingLoop(conn)
	return nil
}

func (h *Hub) connect(ctx context.Context) (*websocket.Conn, error) {
	apiKey, clientCode, jwtToken, feedToken, err := h.angel.StreamCredentials(ctx)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", jwtToken)
	headers.Set("x-api-key", apiKey)
	headers.Set("x-client-code", clientCode)
	headers.Set("x-feed-token", feedToken)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, angelStreamURL, headers)
	if err != nil {
		return nil, fmt.Errorf("angel websocket connect: %w", err)
	}
	return conn, nil
}

func (h *Hub) pingLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		h.mu.Lock()
		current := h.conn == conn
		h.mu.Unlock()
		if !current {
			return
		}
		if err := h.writeControl(conn, websocket.PingMessage, nil); err != nil {
			h.log.Warn().Err(err).Msg("angel websocket ping failed")
			_ = conn.Close()
			return
		}
	}
}

func (h *Hub) writeControl(conn *websocket.Conn, messageType int, data []byte) error {
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return conn.WriteControl(messageType, data, time.Now().Add(writeWait))
}

func (h *Hub) readLoop(conn *websocket.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	defer func() {
		_ = conn.Close()
		shouldReconnect := false
		h.mu.Lock()
		if h.conn == conn {
			h.conn = nil
			for _, sub := range h.subs {
				sub.subscribed = false
				sub.pending = false
			}
			shouldReconnect = len(h.clients) > 0
		}
		h.mu.Unlock()
		if shouldReconnect {
			h.startReconnect()
		}
	}()
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			h.log.Warn().Err(err).Msg("angel websocket read stopped")
			return
		}
		if msgType != websocket.BinaryMessage {
			continue
		}
		tick, ok := h.parseTick(data)
		if ok {
			h.broadcast(tick)
		}
	}
}

func (h *Hub) startReconnect() {
	h.mu.Lock()
	if h.reconnecting {
		h.mu.Unlock()
		return
	}
	h.reconnecting = true
	h.mu.Unlock()

	go h.reconnectLoop()
}

func (h *Hub) reconnectLoop() {
	defer func() {
		h.mu.Lock()
		h.reconnecting = false
		h.mu.Unlock()
	}()

	backoff := reconnectInitial
	for {
		if !h.hasClients() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := h.reconnectOnce(ctx)
		cancel()
		if err == nil {
			return
		}

		h.log.Warn().Err(err).Dur("retry_in", backoff).Msg("angel websocket reconnect failed")
		time.Sleep(backoff)
		backoff *= 2
		if backoff > reconnectMax {
			backoff = reconnectMax
		}
	}
}

func (h *Hub) reconnectOnce(ctx context.Context) error {
	conn, err := h.connect(ctx)
	if err != nil {
		return err
	}

	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	h.conn = conn
	items := h.activeInstrumentsLocked()
	for _, sub := range h.subs {
		sub.pending = sub.refs > 0
	}
	h.mu.Unlock()

	go h.readLoop(conn)
	go h.pingLoop(conn)

	if len(items) == 0 {
		return nil
	}
	if err := h.sendSubscription(actionSubscribe, items); err != nil {
		h.markSubscribeResult(items, false)
		return err
	}
	h.resolveFollowUp(items)
	return nil
}

func (h *Hub) hasClients() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients) > 0
}

func (h *Hub) activeInstrumentsLocked() []subItem {
	items := make([]subItem, 0, len(h.subs))
	for _, sub := range h.subs {
		if sub.refs > 0 {
			items = append(items, subItem{instrument: sub.instrument, mode: sub.mode})
		}
	}
	return items
}

func (h *Hub) dropConnectionIf(conn *websocket.Conn) bool {
	if conn == nil {
		return h.hasClients()
	}

	h.mu.Lock()
	if h.conn != conn {
		shouldReconnect := len(h.clients) > 0
		h.mu.Unlock()
		return shouldReconnect
	}
	h.conn = nil
	for _, sub := range h.subs {
		sub.subscribed = false
		sub.pending = false
	}
	shouldReconnect := len(h.clients) > 0
	h.mu.Unlock()

	_ = conn.Close()
	return shouldReconnect
}

func (h *Hub) parseTick(data []byte) (Tick, bool) {
	if len(data) < 2 {
		return Tick{}, false
	}
	switch int(data[0]) {
	case ModeLTP:
		return h.parseModeLTP(data)
	case ModeSnapQuote:
		return h.parseModeSnapQuote(data)
	default:
		return Tick{}, false
	}
}

func (h *Hub) parseModeLTP(data []byte) (Tick, bool) {
	if len(data) < 51 {
		return Tick{}, false
	}
	exType := int(data[1])
	token := strings.TrimRight(string(data[2:27]), "\x00")
	ltp := float64(binary.LittleEndian.Uint64(data[43:51])) / 100

	it, ok := h.instrumentFor(exType, token)
	if !ok || ltp <= 0 {
		return Tick{}, false
	}
	return Tick{
		InstrumentID: it.ID,
		Symbol:       it.TradingSymbol,
		Exchange:     it.Exchange,
		SymbolToken:  it.SymbolToken,
		Price:        ltp,
		Timestamp:    time.Now(),
	}, true
}

// parseModeSnapQuote parses a mode-3 packet: same LTP offset as mode 1, plus
// volume, open interest, and the best bid/ask picked out of the best-5 depth
// block (10 records of 20 bytes each, tagged buy/sell by a flag field) — see
// Angel SmartAPI v2's websocket binary layout. Used for the option chain,
// where a bare LTP can lag the real live quote (see ModeSnapQuote's doc
// comment).
func (h *Hub) parseModeSnapQuote(data []byte) (Tick, bool) {
	const (
		depthStart   = 147
		depthRecords = 10
		depthRecSize = 20
	)
	if len(data) < depthStart+depthRecords*depthRecSize {
		return Tick{}, false
	}
	exType := int(data[1])
	token := strings.TrimRight(string(data[2:27]), "\x00")
	ltp := float64(binary.LittleEndian.Uint64(data[43:51])) / 100
	volume := int64(binary.LittleEndian.Uint64(data[67:75]))
	oi := float64(binary.LittleEndian.Uint64(data[131:139]))

	// Bid/ask come from the record's POSITION, not its flag field: the
	// depth block is 5 buy records (indices 0-4) followed by 5 sell records
	// (5-9), best-first within each half — the same fixed slicing Angel's
	// own reference client uses.
	//
	// An earlier version of this keyed off the 2-byte flag assuming
	// 0=buy/1=sell. That was backwards, and produced a silent bid/ask swap
	// caught by comparing a live chain against the REST Quote-FULL depth for
	// the same contract (REST: bid 321.60 / ask 325.15 — websocket returned
	// them inverted). The swap was worse than cosmetic: it made bid > ask,
	// which market.EffectivePrice rejects as an invalid quote, so it
	// silently fell back to raw LTP and disabled the whole stale-LTP
	// protection on the websocket path. Position-based parsing has no such
	// ambiguity.
	bestBidOffset := depthStart
	bestAskOffset := depthStart + 5*depthRecSize
	bid := float64(binary.LittleEndian.Uint64(data[bestBidOffset+10:bestBidOffset+18])) / 100
	ask := float64(binary.LittleEndian.Uint64(data[bestAskOffset+10:bestAskOffset+18])) / 100

	it, ok := h.instrumentFor(exType, token)
	if !ok || ltp <= 0 {
		return Tick{}, false
	}
	return Tick{
		InstrumentID: it.ID,
		Symbol:       it.TradingSymbol,
		Exchange:     it.Exchange,
		SymbolToken:  it.SymbolToken,
		Price:        ltp,
		Bid:          bid,
		Ask:          ask,
		Volume:       volume,
		OpenInterest: oi,
		Timestamp:    time.Now(),
	}, true
}

func (h *Hub) instrumentFor(exType int, token string) (instruments.Instrument, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.subs[key(exchangeFromType(exType), token)]
	if !ok {
		return instruments.Instrument{}, false
	}
	return sub.instrument, true
}

func (h *Hub) broadcast(t Tick) {
	k := key(t.Exchange, t.SymbolToken)

	h.lastTickMu.Lock()
	h.lastTicks[k] = t
	h.lastTickMu.Unlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, wanted := range h.clients {
		if _, ok := wanted[k]; !ok {
			continue
		}
		select {
		case ch <- t:
		default:
		}
	}
}

// sendSubscription issues one JSON message per distinct mode present in
// items — Angel's subscribe/unsubscribe request carries a single "mode" for
// the whole message, so a mixed-mode batch (e.g. equity tokens at ModeLTP
// alongside option tokens at ModeSnapQuote in the same addClient/removeClient
// call) has to be split into one message per mode.
func (h *Hub) sendSubscription(action int, items []subItem) error {
	byMode := map[int]map[int][]string{} // mode -> exchangeType -> tokens
	for _, si := range items {
		ex := exchangeType(si.instrument.Exchange)
		if ex == 0 {
			continue
		}
		if byMode[si.mode] == nil {
			byMode[si.mode] = map[int][]string{}
		}
		byMode[si.mode][ex] = append(byMode[si.mode][ex], si.instrument.SymbolToken)
	}
	for mode, byExchange := range byMode {
		if err := h.sendSubscriptionMessage(action, mode, byExchange); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) sendSubscriptionMessage(action, mode int, byExchange map[int][]string) error {
	tokenList := make([]map[string]any, 0, len(byExchange))
	for ex, tokens := range byExchange {
		tokenList = append(tokenList, map[string]any{
			"exchangeType": ex,
			"tokens":       tokens,
		})
	}
	payload := map[string]any{
		"correlationID": fmt.Sprintf("tradenexus-%d", time.Now().UnixNano()),
		"action":        action,
		"params": map[string]any{
			"mode":      mode,
			"tokenList": tokenList,
		},
	}
	b, _ := json.Marshal(payload)

	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("angel websocket not connected")
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		h.dropConnectionIf(conn)
		return err
	}
	return nil
}

func key(exchange, token string) string {
	return strings.ToUpper(exchange) + ":" + token
}

func exchangeType(exchange string) int {
	switch strings.ToUpper(exchange) {
	case "NSE":
		return 1
	case "NFO":
		return 2
	case "BSE":
		return 3
	case "BFO":
		return 4
	default:
		return 0
	}
}

func exchangeFromType(t int) string {
	switch t {
	case 1:
		return "NSE"
	case 2:
		return "NFO"
	case 3:
		return "BSE"
	case 4:
		return "BFO"
	default:
		return ""
	}
}
