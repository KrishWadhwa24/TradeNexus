// Package users manages accounts, watchlists, per-user scanner preferences, and
// Telegram config — the data the signal fan-out routes on.
package users

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a row doesn't exist.
var ErrNotFound = errors.New("not found")

// User is an account.
type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
}

// Watchlist with its instrument ids.
type Watchlist struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	InstrumentIDs []int64 `json:"instrument_ids"`
}

// TelegramConfig for a user.
type TelegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	Enabled  bool   `json:"enabled"`
}

// Repo is the user-domain datastore.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// CreateUser inserts a user (idempotent on email) and returns the id.
func (r *Repo) CreateUser(ctx context.Context, email string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (email) VALUES ($1)
		ON CONFLICT (lower(email)) DO UPDATE SET email = EXCLUDED.email
		RETURNING id::text`, email).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, r.ensureDefaultScannerPrefs(ctx, id)
}

// Register creates a user with a password hash. Returns ErrEmailTaken if the
// email already exists.
func (r *Repo) Register(ctx context.Context, email, passwordHash string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash) VALUES ($1, $2)
		ON CONFLICT (lower(email)) DO NOTHING
		RETURNING id::text`, email, passwordHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEmailTaken
	}
	if err != nil {
		return "", err
	}
	return id, r.ensureDefaultScannerPrefs(ctx, id)
}

// ErrEmailTaken is returned when registering an existing email.
var ErrEmailTaken = errors.New("email already registered")

var defaultScannerPrefs = []string{
	"pine_1d",
	"pine_1w",
	"pine_1m",
	"weekly_1",
	"weekly_2",
	"weekly_3",
	"weekly_4",
	"pattern_downtrend_breakout",
	"pattern_rectangle",
	"pattern_cup_handle",
}

func (r *Repo) ensureDefaultScannerPrefs(ctx context.Context, userID string) error {
	batch := &pgx.Batch{}
	for _, key := range defaultScannerPrefs {
		batch.Queue(`
			INSERT INTO user_scanner_prefs (user_id, scanner_key, enabled)
			VALUES ($1::uuid, $2, TRUE)
			ON CONFLICT (user_id, scanner_key) DO NOTHING`,
			userID, key)
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range defaultScannerPrefs {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// AuthByEmail returns the user id, password hash, and admin flag for login.
func (r *Repo) AuthByEmail(ctx context.Context, email string) (id, hash string, isAdmin bool, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT id::text, password_hash, is_admin FROM users WHERE lower(email) = lower($1)`, email).
		Scan(&id, &hash, &isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, ErrNotFound
	}
	return id, hash, isAdmin, err
}

// EnsureAdmin upserts an admin account with the given credentials. Called on
// boot from ADMIN_EMAIL/ADMIN_PASSWORD so there's always exactly one known
// admin login. The password hash is refreshed each boot and is_admin forced on.
func (r *Repo) EnsureAdmin(ctx context.Context, email, passwordHash string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, is_admin) VALUES ($1, $2, TRUE)
		ON CONFLICT (lower(email)) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, is_admin = TRUE
		RETURNING id::text`, email, passwordHash).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, r.ensureDefaultScannerPrefs(ctx, id)
}

// ListUsers returns all users.
func (r *Repo) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text, email, is_admin FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.IsAdmin); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateWatchlist adds a named watchlist for a user.
func (r *Repo) CreateWatchlist(ctx context.Context, userID, name string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO watchlists (user_id, name) VALUES ($1::uuid, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id::text`, userID, name).Scan(&id)
	return id, err
}

// DeleteWatchlist removes a watchlist owned by a user.
func (r *Repo) DeleteWatchlist(ctx context.Context, userID, watchlistID string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM watchlists
		WHERE id = $1::uuid AND user_id = $2::uuid`, watchlistID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddWatchlistItem adds an instrument to a watchlist.
func (r *Repo) AddWatchlistItem(ctx context.Context, watchlistID string, instrumentID int64) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO watchlist_items (watchlist_id, instrument_id)
		VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING`, watchlistID, instrumentID)
	return err
}

// RemoveWatchlistItem removes an instrument from a watchlist.
func (r *Repo) RemoveWatchlistItem(ctx context.Context, watchlistID string, instrumentID int64) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM watchlist_items WHERE watchlist_id = $1::uuid AND instrument_id = $2`,
		watchlistID, instrumentID)
	return err
}

// ListWatchlists returns a user's watchlists with their instrument ids.
func (r *Repo) ListWatchlists(ctx context.Context, userID string) ([]Watchlist, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT w.id::text, w.name,
		       COALESCE(array_agg(wi.instrument_id) FILTER (WHERE wi.instrument_id IS NOT NULL), '{}')
		FROM watchlists w
		LEFT JOIN watchlist_items wi ON wi.watchlist_id = w.id
		WHERE w.user_id = $1::uuid
		GROUP BY w.id, w.name
		ORDER BY w.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watchlist
	for rows.Next() {
		var wl Watchlist
		if err := rows.Scan(&wl.ID, &wl.Name, &wl.InstrumentIDs); err != nil {
			return nil, err
		}
		out = append(out, wl)
	}
	return out, rows.Err()
}

// SetScannerPrefs upserts a user's scanner enable/disable map.
func (r *Repo) SetScannerPrefs(ctx context.Context, userID string, prefs map[string]bool) error {
	batch := &pgx.Batch{}
	for key, enabled := range prefs {
		batch.Queue(`
			INSERT INTO user_scanner_prefs (user_id, scanner_key, enabled)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT (user_id, scanner_key) DO UPDATE SET enabled = EXCLUDED.enabled`,
			userID, key, enabled)
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range prefs {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// GetScannerPrefs returns a user's scanner preferences.
func (r *Repo) GetScannerPrefs(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT scanner_key, enabled FROM user_scanner_prefs WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		var v bool
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetTelegram upserts a user's Telegram config.
func (r *Repo) SetTelegram(ctx context.Context, userID string, cfg TelegramConfig) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO telegram_configs (user_id, bot_token, chat_id, enabled, updated_at)
		VALUES ($1::uuid, $2, $3, $4, now())
		ON CONFLICT (user_id) DO UPDATE
		SET bot_token = EXCLUDED.bot_token, chat_id = EXCLUDED.chat_id,
		    enabled = EXCLUDED.enabled, updated_at = now()`,
		userID, cfg.BotToken, cfg.ChatID, cfg.Enabled)
	return err
}

// GetTelegram returns a user's Telegram config.
func (r *Repo) GetTelegram(ctx context.Context, userID string) (TelegramConfig, error) {
	var c TelegramConfig
	err := r.pool.QueryRow(ctx,
		`SELECT bot_token, chat_id, enabled FROM telegram_configs WHERE user_id = $1::uuid`, userID).
		Scan(&c.BotToken, &c.ChatID, &c.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}
