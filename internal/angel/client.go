// Package angel is a thin client for Angel One SmartAPI: auth (TOTP+JWT),
// scrip master, and historical candle data. Every historical call passes
// through a shared Redis token bucket so we stay under Angel's rate limits.
package angel

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/ratelimit"
)

// Default Angel endpoints. Overridable via Config for tests (httptest servers).
const (
	defaultAPIBaseURL     = "https://apiconnect.angelbroking.com"
	defaultScripMasterURL = "https://margincalculator.angelbroking.com/OpenAPI_File/files/OpenAPIScripMaster.json"

	pathLogin      = "/rest/auth/angelbroking/user/v1/loginByPassword"
	pathRefresh    = "/rest/auth/angelbroking/jwt/v1/generateTokens"
	pathHistorical = "/rest/secure/angelbroking/historical/v1/getCandleData"

	// jwtTTL is a conservative assumed validity; we refresh before this elapses.
	jwtTTL = 6 * time.Hour
)

// Config configures the Angel client.
type Config struct {
	APIKey     string
	ClientCode string
	PIN        string
	TOTPSecret string

	// Client-identity headers Angel requires. Placeholder values are accepted.
	ClientLocalIP  string
	ClientPublicIP string
	MACAddress     string

	// Overridable base URLs (leave empty for production defaults).
	APIBaseURL          string
	ScripMasterURL      string
	ScripMasterTimeout  time.Duration
	ScripMasterAttempts int
}

// Client talks to Angel SmartAPI.
type Client struct {
	cfg     Config
	http    *http.Client
	limiter *ratelimit.Limiter
	log     zerolog.Logger

	mu        sync.RWMutex
	loginMu   sync.Mutex
	tokens    tokenData
	tokenTime time.Time
}

// StreamCredentials returns the current credentials required by Angel's
// websocket feed, logging in first if needed.
func (c *Client) StreamCredentials(ctx context.Context) (apiKey, clientCode, jwtToken, feedToken string, err error) {
	if err := c.ensureLogin(ctx); err != nil {
		return "", "", "", "", err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.APIKey, c.cfg.ClientCode, c.tokens.JWTToken, c.tokens.FeedToken, nil
}

// New builds a client, filling defaults for any empty config fields.
func New(cfg Config, limiter *ratelimit.Limiter, log zerolog.Logger) *Client {
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = defaultAPIBaseURL
	}
	if cfg.ScripMasterURL == "" {
		cfg.ScripMasterURL = defaultScripMasterURL
	}
	if cfg.ScripMasterTimeout == 0 {
		cfg.ScripMasterTimeout = 5 * time.Minute
	}
	if cfg.ScripMasterAttempts == 0 {
		cfg.ScripMasterAttempts = 3
	}
	if cfg.ClientLocalIP == "" {
		cfg.ClientLocalIP = "127.0.0.1"
	}
	if cfg.ClientPublicIP == "" {
		cfg.ClientPublicIP = "127.0.0.1"
	}
	if cfg.MACAddress == "" {
		cfg.MACAddress = "00:00:00:00:00:00"
	}
	return &Client{
		cfg:     cfg,
		http:    &http.Client{Timeout: 30 * time.Second},
		limiter: limiter,
		log:     log,
	}
}

// commonHeaders sets the mandatory SmartAPI identity headers on a request.
func (c *Client) commonHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-UserType", "USER")
	req.Header.Set("X-SourceID", "WEB")
	req.Header.Set("X-ClientLocalIP", c.cfg.ClientLocalIP)
	req.Header.Set("X-ClientPublicIP", c.cfg.ClientPublicIP)
	req.Header.Set("X-MACAddress", c.cfg.MACAddress)
	req.Header.Set("X-PrivateKey", c.cfg.APIKey)
}

// LoggedIn reports whether we currently hold a (non-expired) JWT.
func (c *Client) LoggedIn() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokens.JWTToken != "" && time.Since(c.tokenTime) < jwtTTL
}

// TokenStatus returns a safe, non-secret snapshot for diagnostics endpoints.
func (c *Client) TokenStatus() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]any{
		"logged_in":         c.tokens.JWTToken != "",
		"jwt_len":           len(c.tokens.JWTToken),
		"feed_token_len":    len(c.tokens.FeedToken),
		"refresh_token_len": len(c.tokens.RefreshToken),
		"acquired_at":       c.tokenTime,
	}
}
