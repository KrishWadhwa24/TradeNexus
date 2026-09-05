// Package api wires the HTTP layer: router, middleware, and handlers.
// Module 1 exposes health/readiness plus a small endpoint to exercise the
// rate limiter from Postman. Feature handlers arrive in later modules.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"tradenexus/internal/analytics"
	"tradenexus/internal/angel"
	"tradenexus/internal/auth"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/deals"
	"tradenexus/internal/engine"
	"tradenexus/internal/fiidii"
	"tradenexus/internal/insights"
	"tradenexus/internal/instruments"
	"tradenexus/internal/investors"
	"tradenexus/internal/ipo"
	"tradenexus/internal/live"
	"tradenexus/internal/notify"
	"tradenexus/internal/optionsalgo"
	"tradenexus/internal/paper"
	"tradenexus/internal/promoter"
	"tradenexus/internal/ratelimit"
	"tradenexus/internal/signals"
	"tradenexus/internal/store"
	"tradenexus/internal/users"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"
)

// Deps bundles everything the HTTP handlers need.
type Deps struct {
	Log            zerolog.Logger
	PG             *store.Postgres
	RDB            *store.Redis
	Limiter        *ratelimit.Limiter
	Angel          *angel.Client
	Instruments    *instruments.Repo
	Candles        *candles.Repo
	Engine         *engine.Service
	Signals        *signals.Repo
	Calendar       *calendar.Service
	Users          *users.Repo
	Notifier       *notify.Dispatcher
	Analytics      *analytics.Service
	Paper          *paper.Service
	Live           *live.Hub
	IPO            *ipo.Service
	Promoter       *promoter.Service
	Deals          *deals.Service
	Investors      *investors.Service
	Insights       *insights.Service
	FiiDii         *fiidii.Service
	OptionsAlgo    *optionsalgo.Repo
	OptionsAlgoSvc *optionsalgo.Service
	JWTSecret      string
	GoogleClientID string
}

// Server holds shared dependencies for handlers.
type Server struct {
	log            zerolog.Logger
	pg             *store.Postgres
	rdb            *store.Redis
	limiter        *ratelimit.Limiter
	angel          *angel.Client
	inst           *instruments.Repo
	candles        *candles.Repo
	engine         *engine.Service
	signals        *signals.Repo
	cal            *calendar.Service
	users          *users.Repo
	notifier       *notify.Dispatcher
	analytics      *analytics.Service
	paper          *paper.Service
	live           *live.Hub
	ipo            *ipo.Service
	promoter       *promoter.Service
	deals          *deals.Service
	investors      *investors.Service
	insights       *insights.Service
	fiidii         *fiidii.Service
	optionsAlgo    *optionsalgo.Repo
	optionsAlgoSvc *optionsalgo.Service
	jwtSecret      string
	googleClientID string

	// scanRunning guards manual scan-all so repeated clicks don't stack.
	scanRunning atomic.Bool
	// refetchRunning guards the admin per-date refetch (also heavy on Angel).
	refetchRunning atomic.Bool
	// reconcileRunning guards the bulk admin reconcile-all endpoint so two
	// overlapping runs (e.g. an admin click racing the daily cron) don't both
	// hammer Angel at once.
	reconcileRunning atomic.Bool
}

// NewServer constructs the API server with its dependencies.
func NewServer(d Deps) *Server {
	// writeJSON is a free function (many small non-method helpers call it
	// too, e.g. parseAdminDate), so it can't take s.log as a receiver.
	// Package-level is the pragmatic way to give it a logger without
	// threading one through every call site.
	errLog = d.Log
	return &Server{
		log:            d.Log,
		pg:             d.PG,
		rdb:            d.RDB,
		limiter:        d.Limiter,
		angel:          d.Angel,
		inst:           d.Instruments,
		candles:        d.Candles,
		engine:         d.Engine,
		signals:        d.Signals,
		cal:            d.Calendar,
		users:          d.Users,
		notifier:       d.Notifier,
		analytics:      d.Analytics,
		paper:          d.Paper,
		live:           d.Live,
		ipo:            d.IPO,
		promoter:       d.Promoter,
		deals:          d.Deals,
		investors:      d.Investors,
		insights:       d.Insights,
		fiidii:         d.FiiDii,
		optionsAlgo:    d.OptionsAlgo,
		optionsAlgoSvc: d.OptionsAlgoSvc,
		jwtSecret:      d.JWTSecret,
		googleClientID: d.GoogleClientID,
	}
}

// ctxKey is the context key type for the authenticated user id.
type ctxKey string

const userIDKey ctxKey = "uid"
const isAdminKey ctxKey = "is_admin"

// authMiddleware validates the Bearer JWT and injects the user id + admin flag
// into context.
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
		ctx = context.WithValue(ctx, isAdminKey, claims.IsAdmin)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// adminOnly rejects requests whose token isn't flagged admin. Must run inside
// the authenticated group (after authMiddleware).
func (s *Server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAdmin, _ := r.Context().Value(isAdminKey).(bool); !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Router builds the chi router with middleware and routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://trade-nexus-smoky.vercel.app", "http://localhost:5173"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(timeoutExcept(30*time.Second, "/live-prices", "/chain-stream", "/admin/reconcile", "/candles/sync", "/sync-scan", "/admin/candles/refetch", "/derivatives/sync", "/paper/trades", "/paper/summary"))

	r.Get("/health", s.handleHealth)
	r.Get("/health/ready", s.handleReady)

	r.Route("/v1", func(r chi.Router) {
		// Public auth endpoints.
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/google", s.handleGoogleLogin)
		r.Get("/users/{uid}/live-prices", s.handleLivePrices)
		r.Get("/public/live-prices", s.handlePublicLivePrices) // landing: snapshot + live ticks (WS)
		r.Get("/users/{uid}/optionsalgo/chain-stream", s.handleOptionChainStream)
		// Admin-curated list shown on the landing page — read is harmless to
		// expose publicly (it must be, since the landing page is pre-auth).
		r.Get("/public/featured-stocks", s.handleListFeaturedStocks)

		// Public, no-login views (IPO/promoter-trades/bulk/block deals) — the
		// shareable-link acquisition surface: none of these read user-specific
		// data, so exposing the same handlers unauthenticated needs no separate
		// "/public/" duplicate route or response shape. Everything mutating
		// (refresh/backfill/send-alert/scan) stays inside the authed/admin-only
		// groups below. The promoter-buying *analyser* (aggregated view) stays
		// behind login, same as the mutual-fund analyser — only the raw
		// promoter-trades feed is public, matching IPO/bulk/block deals.
		r.Get("/ipos", s.handleListIPOs)
		r.Get("/promoter-trades", s.handleListPromoterTrades)
		r.Get("/bulk-deals", s.handleListDeals(deals.Bulk))
		r.Get("/bulk-deals/audit", s.handleDealsAudit(deals.Bulk))
		r.Get("/bulk-deals/{symbol}", s.handleDealDetail(deals.Bulk))
		r.Get("/block-deals", s.handleListDeals(deals.Block))
		r.Get("/block-deals/audit", s.handleDealsAudit(deals.Block))
		r.Get("/block-deals/{symbol}", s.handleDealDetail(deals.Block))

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
			r.Post("/angel/historical", s.handleAngelHistorical)

			// Instruments (Module 2)
			r.Get("/instruments/search", s.handleInstrumentSearch)
			r.Get("/instruments/{id}", s.handleInstrumentGet)

			// Candles (Module 3). Sync fetches from Angel (rate-limited) so it gets
			// a longer timeout than the 30s default.
			r.With(middleware.Timeout(3*time.Minute)).Post("/instruments/{id}/candles/sync", s.handleCandleSync)
			r.Get("/instruments/{id}/candles", s.handleCandleGet)

			// Scanning + signals (Modules 4-6)
			r.Post("/instruments/{id}/scan", s.handleScanInstrument)
			r.With(middleware.Timeout(3*time.Minute)).Post("/instruments/{id}/sync-scan", s.handleSyncScan)
			r.Get("/signals", s.handleSignalsList)

			// Calendar (Module 6)
			r.Get("/calendar/check", s.handleCalendarCheck)

			// Admin / ops (Module 6)
			r.With(middleware.Timeout(35*time.Minute)).Post("/admin/reconcile", s.handleReconcile)
			r.With(middleware.Timeout(35*time.Minute)).Post("/admin/scan-all", s.handleScanAll)
			r.Post("/admin/cleanup", s.handleCleanup)
			r.Post("/admin/holidays", s.handleAddHolidays)

			// Admin-only candle tools (count / delete / refetch a specific day).
			r.Group(func(r chi.Router) {
				r.Use(s.adminOnly)
				r.Get("/admin/candles", s.handleCandleCountByDate)
				r.Delete("/admin/candles", s.handleDeleteCandlesByDate)
				r.With(middleware.Timeout(65*time.Minute)).Post("/admin/candles/refetch", s.handleRefetchCandlesByDate)
				r.Post("/admin/dispatch/force", s.handleForceDispatch)

				// Stock universe: re-download the Angel scrip master and upsert
				// NSE/BSE cash equities (picks up newly listed IPOs, etc.).
				r.Post("/angel/scripmaster/sync", s.handleScripMasterSync)
				r.Post("/angel/derivatives/sync", s.handleDerivativesSync)

				// IPO admin: refresh the feed now, or push a manual "Apply".
				r.Post("/admin/ipos/refresh", s.handleRefreshIPOs)
				r.Post("/admin/ipos/{id}/apply", s.handleIPOAdminApply)
				r.Post("/admin/ipos/{id}/clear-signal", s.handleIPOClearSignal)

				// Promoter trades admin: force-send a specific trade's Telegram alert.
				r.Post("/admin/promoter-trades/{id}/send-alert", s.handlePromoterSendAlert)
				r.Post("/admin/promoter-buying/backfill", s.handleBackfillPromoterBuying)
				r.Post("/admin/deals/refresh", s.handleRefreshDeals)
				r.Post("/admin/deals/{type}/{symbol}/send-alert", s.handleDealsSendAlert)
				r.Post("/admin/mutual-funds/backfill", s.handleBackfillMutualFunds)
				r.Post("/admin/big-investors/refresh", s.handleRefreshInvestors)

				// FII/DII admin: force-send the currently cached snapshot now.
				r.Post("/admin/fii-dii/send-alert", s.handleFiiDiiSendAlert)

				// Featured stocks: the admin-curated list shown on the landing page.
				r.Post("/admin/featured-stocks", s.handleAddFeaturedStock)
				r.Delete("/admin/featured-stocks/{id}", s.handleRemoveFeaturedStock)

				// Options-algo verification: read-only view of the 1-minute
				// candle feed (see internal/optionsalgo) — proves the
				// backfill/live-refresh loop is actually running.
				r.Get("/admin/optionsalgo/candles", s.handleOptionsAlgoCandles)

				// Phase 1 live-verification for the market-direction engine.
				r.Get("/admin/optionsalgo/direction", s.handleOptionsAlgoDirection)

				// Phase 2 live-verification for option chain + strike selection.
				r.Get("/admin/optionsalgo/option-chain", s.handleOptionsAlgoOptionChain)

				// Phase 3 live-verification for the full direction -> chain
				// -> select -> entry pipeline.
				r.Get("/admin/optionsalgo/entry", s.handleOptionsAlgoEntry)

				// Phase 4b: the real execution bridge — places/manages
				// actual paper trades. Admin only, for live verification
				// before automatic wiring.
				r.Post("/admin/optionsalgo/enter", s.handleOptionsAlgoEnter)
				r.Post("/admin/optionsalgo/manage", s.handleOptionsAlgoManage)

				// Phase 4b: settings — every script constant, frontend-
				// editable. One shared row for the whole algo (not
				// per-user), so this stays admin-gated like the rest of
				// this group.
				r.Get("/admin/optionsalgo/config", s.handleGetAlgoConfig)
				r.Put("/admin/optionsalgo/config", s.handleUpdateAlgoConfig)

				// Phase 5: full decision/audit log.
				r.Get("/admin/optionsalgo/decisions", s.handleOptionsAlgoDecisions)

				// Phase 0 live-verification for the two new Angel
				// integrations (quote-full, option greeks) — manual testing
				// only, no real callers, same purpose as /admin/angel/historical.
				r.Get("/admin/angel/quote-full", s.handleAngelQuoteFullTest)
				r.Get("/admin/angel/option-greeks", s.handleAngelOptionGreeksTest)
			})

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

			// "Scan now" is open to every logged-in user (cooldown-guarded
			// inside the service). The read-only promoter-trades list is
			// registered above, outside authMiddleware — see the "Public,
			// no-login views" block.
			r.Post("/promoter-trades/scan", s.handlePromoterScanNow)

			// Promoter buying analyser (aggregated view) + mutual fund
			// analyser: both stay behind login, unlike the raw feeds above.
			r.Get("/promoter-buying", s.handleListPromoterBuying)
			r.Get("/promoter-buying/{symbol}", s.handlePromoterBuyingDetail)
			r.Get("/promoter-buying/{symbol}/history", s.handlePromoterPersonHistory)
			r.Get("/mutual-funds", s.handleListMutualFunds)
			r.Get("/mutual-funds/{fund}", s.handleMutualFundDetail)
			r.Get("/big-investors", s.handleListInvestors)
			r.Get("/big-investors/{name}", s.handleInvestorDetail)

			r.Get("/insights/performance", s.handleInsightsPerformance)
			r.Get("/insights/breadth", s.handleInsightsBreadth)
			r.Get("/insights/confluence", s.handleInsightsConfluence)
			r.Get("/insights/fii-dii", s.handleFiiDiiLatest)
			r.Get("/insights/fii-dii/history", s.handleFiiDiiHistory)

			// Market data (Module 9): trending + params + dashboard
			r.Get("/market/trending", s.handleTrending)
			r.Get("/instruments/{id}/params", s.handleInstrumentParams)
			r.Get("/stocks/{id}/360", s.handleStock360)
			r.Get("/instruments/{id}/coverage", s.handleCoverage)
			r.Get("/users/{uid}/dashboard", s.handleDashboard)
			r.Get("/users/{uid}/coverage", s.handleUserCoverage)

			// Paper trading (Module 9)
			r.Put("/users/{uid}/paper/capital", s.handleSetCapital)
			r.Put("/users/{uid}/paper/algo-capital", s.handleSetAlgoCapital)
			r.Put("/users/{uid}/paper/algo-enabled", s.handleSetAlgoEnabled)
			r.Get("/users/{uid}/paper/account", s.handleGetAccount)
			r.Post("/users/{uid}/paper/trades", s.handleBuy)
			r.Post("/users/{uid}/paper/trades/open", s.handleOpenPosition)
			r.Get("/users/{uid}/paper/trades", s.handleListTrades)
			r.Get("/users/{uid}/paper/summary", s.handlePaperSummary)
			r.Get("/users/{uid}/paper/algo-summary", s.handleAlgoSummary)
			r.Get("/users/{uid}/paper/algo-stats", s.handleAlgoStats)
			r.Get("/users/{uid}/paper/algo-daily-pnl", s.handleAlgoDailyPnL)
			r.Post("/paper/trades/{tradeId}/close", s.handleCloseTrade)
			r.Post("/paper/trades/{tradeId}/convert", s.handleConvertToDelivery)
			r.Post("/paper/trades/{tradeId}/cancel", s.handleCancelScheduled)

			// Options-algo: any logged-in user can view the live option
			// chain and buy manually (via the existing generic
			// /paper/trades/open above, under their own regular balance —
			// this endpoint is read-only).
			r.Get("/optionsalgo/chain", s.handleOptionChainPublic)
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

// errLog backs writeJSON's automatic 5xx logging — see NewServer. Every
// handler error response used to be visible only in the HTTP response body,
// never in the server's own logs, so a real failure only ever surfaced if a
// user happened to notice and report the on-screen error message.
var errLog zerolog.Logger

// writeJSON is a small helper for JSON responses. Any 5xx response is also
// logged server-side — the existing chi request logger (see api.go's
// r.Use(middleware.Logger)-equivalent) already logs method+path+status for
// every request, so this line is what you actually grep for: the real error
// message, not just "status=500". Client 4xx responses are normal, expected
// control flow and stay quiet.
func writeJSON(w http.ResponseWriter, status int, v any) {
	if status >= 500 {
		errLog.Error().Int("status", status).Interface("body", v).Msg("api: request failed")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
