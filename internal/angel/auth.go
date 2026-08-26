package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pquerna/otp/totp"
)

// loginRetryCooldown bounds how often we'll re-attempt a login after it just
// failed. Without this, a batch job iterating hundreds of instruments (e.g.
// ReconcileAll) would retry the same doomed login once per instrument — each
// paying a full network/TLS timeout — turning one outage into a very long one.
const loginRetryCooldown = 30 * time.Second

// currentTOTP generates the 6-digit code from the configured secret.
func (c *Client) currentTOTP() (string, error) {
	if c.cfg.TOTPSecret == "" {
		return "", fmt.Errorf("angel: TOTP secret not configured")
	}
	code, err := totp.GenerateCode(c.cfg.TOTPSecret, time.Now())
	if err != nil {
		return "", fmt.Errorf("angel: generate totp: %w", err)
	}
	return code, nil
}

// Login authenticates with client code + PIN + TOTP and stores the tokens.
func (c *Client) Login(ctx context.Context) error {
	if c.cfg.ClientCode == "" || c.cfg.PIN == "" {
		return fmt.Errorf("angel: client code / PIN not configured")
	}
	code, err := c.currentTOTP()
	if err != nil {
		return err
	}

	body, _ := json.Marshal(loginRequest{
		ClientCode: c.cfg.ClientCode,
		Password:   c.cfg.PIN,
		TOTP:       code,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.APIBaseURL+pathLogin, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.commonHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("angel login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var lr loginResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		return fmt.Errorf("angel login decode (status %d): %w", resp.StatusCode, err)
	}
	if !lr.Status || lr.Data.JWTToken == "" {
		return fmt.Errorf("angel login failed: %s (%s)", lr.Message, lr.ErrorCode)
	}

	c.mu.Lock()
	c.tokens = lr.Data
	c.tokenTime = time.Now()
	c.mu.Unlock()

	c.log.Info().Msg("angel: login successful")
	return nil
}

// RefreshTokens exchanges the refresh token for fresh JWT/feed tokens.
func (c *Client) RefreshTokens(ctx context.Context) error {
	c.mu.RLock()
	refresh := c.tokens.RefreshToken
	jwt := c.tokens.JWTToken
	c.mu.RUnlock()
	if refresh == "" {
		return fmt.Errorf("angel: no refresh token; call Login first")
	}

	body, _ := json.Marshal(refreshRequest{RefreshToken: refresh})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.APIBaseURL+pathRefresh, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("angel refresh: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var lr loginResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		return fmt.Errorf("angel refresh decode (status %d): %w", resp.StatusCode, err)
	}
	if !lr.Status || lr.Data.JWTToken == "" {
		return fmt.Errorf("angel refresh failed: %s (%s)", lr.Message, lr.ErrorCode)
	}

	c.mu.Lock()
	c.tokens = lr.Data
	c.tokenTime = time.Now()
	c.mu.Unlock()
	return nil
}

// ensureLogin logs in if we don't currently hold a valid JWT. Concurrent
// callers serialize on loginMu: only the first actually calls Login(); the
// rest wait, then reuse its outcome (success reflected in LoggedIn(), failure
// reflected via the cooldown below) instead of each firing their own request.
// If the previous attempt failed recently, it fails fast instead of retrying.
func (c *Client) ensureLogin(ctx context.Context) error {
	if c.LoggedIn() {
		return nil
	}
	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	// Re-check: whoever held loginMu before us may have already logged in.
	if c.LoggedIn() {
		return nil
	}
	return c.doLogin(ctx)
}

// ForceLogin logs in fresh even if the current session still looks valid —
// used by the manual admin re-login endpoint. Still serialized via loginMu
// (and still cooldown-gated) so it can't race a concurrent ensureLogin or
// refreshOrLogin call from a worker pool.
func (c *Client) ForceLogin(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	return c.doLogin(ctx)
}

// doLogin performs the cooldown-gated Login call and records the outcome.
// Callers must hold loginMu.
func (c *Client) doLogin(ctx context.Context) error {
	c.mu.RLock()
	failAt, lastErr := c.loginFailAt, c.loginErr
	c.mu.RUnlock()
	if !failAt.IsZero() && time.Since(failAt) < loginRetryCooldown {
		return fmt.Errorf("angel: login on cooldown after recent failure (%s ago): %w",
			time.Since(failAt).Round(time.Second), lastErr)
	}

	err := c.Login(ctx)
	c.mu.Lock()
	if err != nil {
		c.loginFailAt = time.Now()
		c.loginErr = err
	} else {
		c.loginFailAt = time.Time{}
		c.loginErr = nil
	}
	c.mu.Unlock()
	return err
}

// jwt returns the current JWT (may be empty).
func (c *Client) jwt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokens.JWTToken
}
