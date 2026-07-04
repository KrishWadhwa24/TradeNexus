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

// ensureLogin logs in if we don't currently hold a valid JWT.
func (c *Client) ensureLogin(ctx context.Context) error {
	if c.LoggedIn() {
		return nil
	}
	return c.Login(ctx)
}

// jwt returns the current JWT (may be empty).
func (c *Client) jwt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokens.JWTToken
}
