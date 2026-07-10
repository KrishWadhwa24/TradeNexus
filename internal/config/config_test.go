package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort default = %q, want 8080", cfg.HTTPPort)
	}
	if cfg.AppEnv != "local" || !cfg.IsLocal() {
		t.Errorf("AppEnv/IsLocal wrong: env=%q local=%v", cfg.AppEnv, cfg.IsLocal())
	}
	if cfg.NotifyWindowDays != 7 {
		t.Errorf("NotifyWindowDays default = %d, want 7", cfg.NotifyWindowDays)
	}
	if cfg.IntradayCacheInterval.Minutes() != 20 {
		t.Errorf("IntradayCacheInterval default = %v, want 20m", cfg.IntradayCacheInterval)
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays default = %d, want 30", cfg.RetentionDays)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("APP_ENV", "prod")
	t.Setenv("HTTP_PORT", "9999")
	t.Setenv("NOTIFY_WINDOW_DAYS", "3")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IsLocal() {
		t.Error("prod env should not be local")
	}
	if cfg.HTTPPort != "9999" || cfg.NotifyWindowDays != 3 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}
