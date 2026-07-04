package angel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestLogin_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the required identity headers are present.
		if r.Header.Get("X-PrivateKey") != "my-api-key" {
			t.Errorf("X-PrivateKey = %q", r.Header.Get("X-PrivateKey"))
		}
		// Verify body carries clientcode + a 6-digit totp.
		var body loginRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ClientCode != "C123" {
			t.Errorf("clientcode = %q", body.ClientCode)
		}
		if len(body.TOTP) != 6 {
			t.Errorf("totp = %q (want 6 digits)", body.TOTP)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": true, "message": "SUCCESS", "errorcode": "",
			"data": {"jwtToken":"jwt-abc","refreshToken":"ref-abc","feedToken":"feed-abc"}
		}`))
	}))
	defer srv.Close()

	c := New(Config{
		APIKey:     "my-api-key",
		ClientCode: "C123",
		PIN:        "1234",
		TOTPSecret: "JBSWY3DPEHPK3PXP", // valid base32 test secret
		APIBaseURL: srv.URL,
	}, nil, zerolog.Nop())

	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !c.LoggedIn() {
		t.Fatal("expected LoggedIn() true after login")
	}
	if c.jwt() != "jwt-abc" {
		t.Errorf("jwt = %q, want jwt-abc", c.jwt())
	}
}

func TestLogin_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status": false, "message":"Invalid TOTP","errorcode":"AB1050","data":{}}`))
	}))
	defer srv.Close()

	c := New(Config{
		ClientCode: "C123", PIN: "1234",
		TOTPSecret: "JBSWY3DPEHPK3PXP", APIBaseURL: srv.URL,
	}, nil, zerolog.Nop())

	if err := c.Login(context.Background()); err == nil {
		t.Fatal("expected login error on status:false")
	}
}
