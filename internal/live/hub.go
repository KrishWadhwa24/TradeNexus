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
	modeLTP        = 1

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
	Timestamp    time.Time `json:"timestamp"`
}

type Hub struct {
	angel *angel.Client
	log   zerolog.Logger

	mu           sync.Mutex
	conn         *websocket.Conn
	writeMu      sync.Mutex
	clients      map[chan Tick]map[string]instruments.Instrument
	subs         map[string]*subscription
	reconnecting bool

	lastTickMu sync.RWMutex
	lastTicks  map[string]Tick
}

type subscription struct {
	instrument instruments.Instrument
	refs       int
	subscribed bool
	pending    bool
}

func NewHub(angelClient *angel.Client, log zerolog.Logger) *Hub {
	return &Hub{
		angel:     angelClient,
		log:       log,
		clients:   map[chan Tick]map[string]instruments.Instrument{},
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

func (h *Hub) Subscribe(ctx context.Context, items []instruments.Instrument) (<-chan Tick, func(), error) {
	ch := make(chan Tick, 64)
	wanted := map[string]instruments.Instrument{}
	for _, it := range items {
		if exchangeType(it.Exchange) == 0 || it.SymbolToken == "" {
			continue
		}
		wanted[key(it.Exchange, it.SymbolToken)] = it
	}
	if len(wanted) == 0 {
		close(ch)
		return ch, func() {}, nil
	}

	toSubscribe := h.addClient(ch, wanted)

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
	if len(toSubscribe) > 0 {
		if err := h.sendSubscription(actionSubscribe, toSubscribe); err != nil {
			h.markSubscribeResult(toSubscribe, false)
			cancel()
			if h.hasClients() {
				h.startReconnect()
			}
			return nil, nil, err
		}
		h.markSubscribeResult(toSubscribe, true)
	}
	return ch, cancel, nil
}

func (h *Hub) addClient(ch chan Tick, wanted map[string]instruments.Instrument) []instruments.Instrument {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[ch] = wanted
	var toSubscribe []instruments.Instrument
	for k, it := range wanted {
		sub, ok := h.subs[k]
		if !ok {
			sub = &subscription{instrument: it}
			h.subs[k] = sub
		}
		sub.instrument = it
		sub.refs++
		if !sub.subscribed && !sub.pending {
			sub.pending = true
			toSubscribe = append(toSubscribe, it)
		}
	}
	return toSubscribe
}

func (h *Hub) removeClient(ch chan Tick, wanted map[string]instruments.Instrument) {
	var toUnsubscribe []instruments.Instrument

	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	for k, it := range wanted {
		sub, ok := h.subs[k]
		if !ok {
			continue
		}
		if sub.refs > 0 {
			sub.refs--
		}
		if sub.refs == 0 {
			toUnsubscribe = append(toUnsubscribe, it)
			delete(h.subs, k)
		}
	}
	h.mu.Unlock()

	if len(toUnsubscribe) > 0 {
		if err := h.sendSubscription(actionUnsubscribe, toUnsubscribe); err != nil {
			h.log.Debug().Err(err).Msg("angel websocket unsubscribe skipped")
		}
	}
}

func (h *Hub) markSubscribeResult(items []instruments.Instrument, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, it := range items {
		sub := h.subs[key(it.Exchange, it.SymbolToken)]
		if sub == nil {
			continue
		}
		sub.pending = false
		if ok {
			sub.subscribed = true
		}
	}
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
		sub.pending = len(items) > 0
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
	h.markSubscribeResult(items, true)
	return nil
}

func (h *Hub) hasClients() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients) > 0
}

func (h *Hub) activeInstrumentsLocked() []instruments.Instrument {
	items := make([]instruments.Instrument, 0, len(h.subs))
	for _, sub := range h.subs {
		if sub.refs > 0 {
			items = append(items, sub.instrument)
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
	if len(data) < 51 || int(data[0]) != modeLTP {
		return Tick{}, false
	}
	exType := int(data[1])
	token := strings.TrimRight(string(data[2:27]), "\x00")
	ltp := float64(binary.LittleEndian.Uint64(data[43:51])) / 100

	h.mu.Lock()
	sub, ok := h.subs[key(exchangeFromType(exType), token)]
	var it instruments.Instrument
	if ok {
		it = sub.instrument
	}
	h.mu.Unlock()
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

func (h *Hub) sendSubscription(action int, items []instruments.Instrument) error {
	byExchange := map[int][]string{}
	for _, it := range items {
		ex := exchangeType(it.Exchange)
		if ex == 0 {
			continue
		}
		byExchange[ex] = append(byExchange[ex], it.SymbolToken)
	}
	if len(byExchange) == 0 {
		return nil
	}
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
			"mode":      modeLTP,
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
	case "BSE":
		return 3
	default:
		return 0
	}
}

func exchangeFromType(t int) string {
	switch t {
	case 1:
		return "NSE"
	case 3:
		return "BSE"
	default:
		return ""
	}
}
