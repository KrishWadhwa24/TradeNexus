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

	mu          sync.Mutex
	conn        *websocket.Conn
	writeMu     sync.Mutex
	clients     map[chan Tick]map[string]instruments.Instrument
	instruments map[string]instruments.Instrument
	subscribed  map[string]bool
}

func NewHub(angelClient *angel.Client, log zerolog.Logger) *Hub {
	return &Hub{
		angel:       angelClient,
		log:         log,
		clients:     map[chan Tick]map[string]instruments.Instrument{},
		instruments: map[string]instruments.Instrument{},
		subscribed:  map[string]bool{},
	}
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

	h.mu.Lock()
	h.clients[ch] = wanted
	var toSubscribe []instruments.Instrument
	for k, it := range wanted {
		h.instruments[k] = it
		if !h.subscribed[k] {
			toSubscribe = append(toSubscribe, it)
		}
	}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}

	if err := h.ensureConnected(ctx); err != nil {
		cancel()
		return nil, nil, err
	}
	if len(toSubscribe) > 0 {
		if err := h.sendSubscription(toSubscribe); err != nil {
			cancel()
			return nil, nil, err
		}
		h.mu.Lock()
		for _, it := range toSubscribe {
			h.subscribed[key(it.Exchange, it.SymbolToken)] = true
		}
		h.mu.Unlock()
	}
	return ch, cancel, nil
}

func (h *Hub) ensureConnected(ctx context.Context) error {
	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	apiKey, clientCode, jwtToken, feedToken, err := h.angel.StreamCredentials(ctx)
	if err != nil {
		return err
	}
	headers := http.Header{}
	headers.Set("Authorization", jwtToken)
	headers.Set("x-api-key", apiKey)
	headers.Set("x-client-code", clientCode)
	headers.Set("x-feed-token", feedToken)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, angelStreamURL, headers)
	if err != nil {
		return fmt.Errorf("angel websocket connect: %w", err)
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
	return nil
}

func (h *Hub) readLoop(conn *websocket.Conn) {
	defer func() {
		_ = conn.Close()
		h.mu.Lock()
		if h.conn == conn {
			h.conn = nil
			h.subscribed = map[string]bool{}
		}
		h.mu.Unlock()
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

func (h *Hub) parseTick(data []byte) (Tick, bool) {
	if len(data) < 51 || int(data[0]) != modeLTP {
		return Tick{}, false
	}
	exType := int(data[1])
	token := strings.TrimRight(string(data[2:27]), "\x00")
	ltp := float64(binary.LittleEndian.Uint64(data[43:51])) / 100

	h.mu.Lock()
	it, ok := h.instruments[key(exchangeFromType(exType), token)]
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
	h.mu.Lock()
	defer h.mu.Unlock()
	k := key(t.Exchange, t.SymbolToken)
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

func (h *Hub) sendSubscription(items []instruments.Instrument) error {
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
		"action":        1,
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
	return conn.WriteMessage(websocket.TextMessage, b)
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
