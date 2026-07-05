// Package api wires the HTTP layer: router, middleware, and handlers.
// Module 1 exposes health/readiness plus a small endpoint to exercise the
// rate limiter from Postman. Feature handlers arrive in later modules.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"tradenexus/internal/analytics"
	"tradenexus/internal/angel"
	"tradenexus/internal/auth"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/engine"
	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
	"tradenexus/internal/notify"
	"tradenexus/internal/paper"
	"tradenexus/internal/ratelimit"
	"tradenexus/internal/signals"
	"tradenexus/internal/store"
	"tradenexus/internal/users"
)

// Deps bundles everything the HTTP handlers need.
type Deps struct {
	Log         zerolog.Logger
	PG          *store.Postgres
	RDB         *store.Redis
	Limiter     *ratelimit.Limiter
	Angel       *angel.Client
	Instruments *instruments.Repo
	Candles     *candles.Repo
	Engine      *engine.Service
	Signals     *signals.Repo
	Calendar    *calendar.Service
	Users       *users.Repo
	Notifier    *notify.Dispatcher
	Analytics   *analytics.Service
	Paper       *paper.Service
	Live        *live.Hub
	JWTSecret   string
}

// Server holds shared dependencies for handlers.
type Server struct {
	log       zerolog.Logger
	pg        *store.Postgres
	rdb       *store.Redis
	limiter   *ratelimit.Limiter
	angel     *angel.Client
	inst      *instruments.Repo
	candles   *candles.Repo
	engine    *engine.Service
	signals   *signals.Repo
	cal       *calendar.Service
	users     *users.Repo
	notifier  *notify.Dispatcher
	analytics *analytics.Service
	paper     *paper.Service
	live      *live.Hub
	jwtSecret string
}

// NewServer constructs the API server with its dependencies.
func NewServer(d Deps) *Server {
	return &Server{
		log:       d.Log,
		pg:        d.PG,
		rdb:       d.RDB,
		limiter:   d.Limiter,
		angel:     d.Angel,
		inst:      d.Instruments,
		candles:   d.Candles,
		engine:    d.Engine,
		signals:   d.Signals,
		cal:       d.Calendar,
		users:     d.Users,
		notifier:  d.Notifier,
		analytics: d.Analytics,
		paper:     d.Paper,
		live:      d.Live,
		jwtSecret: d.JWTSecret,
	}
}

// ctxKey is the context key type for the authenticated user id.
type ctxKey string

const userIDKey ctxKey = "uid"

// authMiddleware validates the Bearer JWT and injects the user id into context.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}
		claims, err := auth.Parse(s.jwtSecret, strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Router builds the chi router with middleware and routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(timeoutExcept(30*time.Second, "/live-prices", "/admin/reconcile"))

	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)

	r.Route("/v1", func(r chi.Router) {
		// Public auth endpoints.
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Get("/users/{uid}/live-prices", s.handleLivePrices)

		// Everything below requires a valid JWT.
		r.Group(func(r chi.Router) {
			r.Use(s.authMiddleware)
			r.Get("/me", s.handleMe)

			// Demo endpoint so you can hammer the limiter from Postman and watch
			// it flip from allowed:true to allowed:false with a retry hint.
			r.Post("/ratelimit/try", s.handleRateLimitTry)

			// Angel client (Module 2)
			r.Post("/angel/login", s.handleAngelLogin)
			r.Get("/angel/status", s.handleAngelStatus)
			r.Post("/angel/scripmaster/sync", s.handleScripMasterSync)
			r.Post("/angel/historical", s.handleAngelHistorical)

			// Instruments (Module 2)
			r.Get("/instruments/search", s.handleInstrumentSearch)
			r.Get("/instruments/{id}", s.handleInstrumentGet)

			// Candles (Module 3)
			r.Post("/instruments/{id}/candles/sync", s.handleCandleSync)
			r.Get("/instruments/{id}/candles", s.handleCandleGet)

			// Scanning + signals (Modules 4-6)
			r.Post("/instruments/{id}/scan", s.handleScanInstrument)
			r.Post("/instruments/{id}/sync-scan", s.handleSyncScan)
			r.Get("/signals", s.handleSignalsList)

			// Calendar (Module 6)
			r.Get("/calendar/check", s.handleCalendarCheck)

			// Admin / ops (Module 6)
			r.With(middleware.Timeout(35*time.Minute)).Post("/admin/reconcile", s.handleReconcile)
			r.Post("/admin/scan-all", s.handleScanAll)
			r.Post("/admin/cleanup", s.handleCleanup)
			r.Post("/admin/holidays", s.handleAddHolidays)

			// Users, watchlists, prefs, telegram (Module 7)
			r.Post("/users", s.handleCreateUser)
			r.Get("/users", s.handleListUsers)
			r.Post("/users/{uid}/watchlists", s.handleCreateWatchlist)
			r.Get("/users/{uid}/watchlists", s.handleListWatchlists)
			r.Delete("/users/{uid}/watchlists/{wid}", s.handleDeleteWatchlist)
			r.Post("/watchlists/{wid}/items", s.handleAddWatchlistItem)
			r.Delete("/watchlists/{wid}/items/{instrumentId}", s.handleRemoveWatchlistItem)
			r.Put("/users/{uid}/scanner-prefs", s.handleSetScannerPrefs)
			r.Get("/users/{uid}/scanner-prefs", s.handleGetScannerPrefs)
			r.Put("/users/{uid}/telegram", s.handleSetTelegram)
			r.Get("/users/{uid}/telegram", s.handleGetTelegram)

			// Notification testing (Module 7)
			r.Post("/telegram/test", s.handleTelegramTest)
			r.Get("/signals/{id}/recipients", s.handleSignalRecipients)
			r.Post("/admin/dispatch", s.handleDispatch)

			// Analytics + Excel export (Module 8)
			r.Get("/analytics/summary", s.handleAnalyticsSummary)
			r.Get("/analytics/export.xlsx", s.handleAnalyticsExport)

			// Market data (Module 9): trending + params + dashboard
			r.Get("/market/trending", s.handleTrending)
			r.Get("/instruments/{id}/params", s.handleInstrumentParams)
			r.Get("/instruments/{id}/coverage", s.handleCoverage)
			r.Get("/users/{uid}/dashboard", s.handleDashboard)
			r.Get("/users/{uid}/coverage", s.handleUserCoverage)

			// Paper trading (Module 9)
			r.Put("/users/{uid}/paper/capital", s.handleSetCapital)
			r.Get("/users/{uid}/paper/account", s.handleGetAccount)
			r.Post("/users/{uid}/paper/trades", s.handleBuy)
			r.Get("/users/{uid}/paper/trades", s.handleListTrades)
			r.Get("/users/{uid}/paper/summary", s.handlePaperSummary)
			r.Post("/paper/trades/{tradeId}/close", s.handleCloseTrade)
		}) // end protected group
	})

	return r
}

func timeoutExcept(timeout time.Duration, suffixes ...string) func(http.Handler) http.Handler {
	withTimeout := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		timed := withTimeout(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, suffix := range suffixes {
				if strings.HasSuffix(r.URL.Path, suffix) {
					next.ServeHTTP(w, r)
					return
				}
			}
			timed.ServeHTTP(w, r)
		})
	}
}

// requestLogger is a tiny zerolog-based access log middleware.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, req.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, req)
		s.log.Info().
			Str("method", req.Method).
			Str("path", req.URL.Path).
			Int("status", ww.Status()).
			Dur("took", time.Since(start)).
			Str("reqid", middleware.GetReqID(req.Context())).
			Msg("http")
	})
}

// writeJSON is a small helper for JSON responses.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
