# TradeNexus

Unified stock scanning + paper-trading platform. Go · PostgreSQL · Redis · React · Angel One SmartAPI.

Full design: see [`IMPLEMENTATION_PLAN.md`](./IMPLEMENTATION_PLAN.md) and [`architecture.svg`](./architecture.svg).

The codebase is built **module by module** — each is runnable and testable before the next is added.

---

## Module status

| # | Module | Status |
|---|--------|--------|
| 1 | Foundation: config, Postgres/Redis, migrations, **Angel rate limiter**, health API | ✅ |
| 2 | Angel client: auth (TOTP+JWT), scrip master, historical fetch | ✅ |
| 3 | Candle store + weekly/monthly aggregation | ✅ |
| 4 | Indicator engine (EMA/SMA/RSI/ATR/highest/lowest/crossover) | ✅ |
| 5 | Scanner engine: Pine (D/W/M) + 4 weekly scanners (N/4) | ✅ |
| 6 | Signals audit store, trading calendar, reconciliation, scheduler | ✅ |
| 7 | Users/watchlists/prefs, fan-out + Telegram notifications | ✅ |
| 8 | Analytics dashboard + Excel export | ✅ |
| 9 | Paper trading + market data (trending, params, live price) | ✅ |
| 10 | React frontend (dark/light, login, watchlist, redesign) | ✅ |
| 11 | JWT auth (register/login, protected API) | ✅ |

---

## Module 1 — what's here

- **Config** (`internal/config`) — env-driven, `.env` supported.
- **Logger** (`internal/logger`) — zerolog, pretty in local mode.
- **Postgres** (`internal/store/postgres.go`) — pgx pool + ping.
- **Redis** (`internal/store/redis.go`) — go-redis + ping.
- **Migrations** (`migrations/`) — embedded, auto-applied on boot (users, instruments, watchlists, scanner prefs).
- **Rate limiter** (`internal/ratelimit`) — **Redis token bucket** sized to Angel's limits. One shared budget for every caller. Atomic Lua refill+take. `Allow` (non-blocking) and `Wait` (blocking/queueing).
- **HTTP API** (`internal/api`) — health, readiness, and a rate-limiter demo endpoint.

---

## Prerequisites

- Go 1.22+
- Docker (for local Postgres + Redis)

## Run it

```bash
# 1. Config
cp .env.example .env          # defaults already match docker-compose

# 2. Dependencies (Postgres + Redis)
make deps                     # docker compose up -d

# 3. Go modules
make tidy                     # go mod tidy

# 4. Start the server (auto-applies migrations)
make run                      # go run ./cmd/server
```

Server boots on `http://localhost:8080`. On success you'll see logs for
migrations, postgres, redis, the rate limiter, and `http listening`.

## Test it

```bash
make deps      # Redis must be up for the limiter test
make test      # go test ./...   (rate-limiter test skips if no Redis)
```

---

## Postman / curl smoke tests

Import [`postman_collection.json`](./postman_collection.json), or use curl:

**Liveness**
```bash
curl -s localhost:8080/health
# {"status":"ok"}
```

**Readiness** (checks Postgres + Redis)
```bash
curl -s localhost:8080/health/ready
# {"status":"ready","checks":{"postgres":"ok","redis":"ok"}}
```
Stop Redis (`docker compose stop redis`) and call again → `503` with
`"redis":"down: ..."`. Restart to recover.

**Rate limiter** (defaults: rate 3/s, burst 3)
```bash
curl -s -X POST localhost:8080/v1/ratelimit/try
# {"allowed":true,"retry_after_ms":0}
```
Fire it ~5 times fast (or use Postman's Runner with 10 iterations, 0 delay):
the first 3 return `allowed:true`, then `allowed:false` with a
`retry_after_ms` hint. Wait a second and tokens refill. This is exactly the
budget the Angel historical fetcher will share in Module 2.

Tune limits in `.env`: `ANGEL_HIST_RATE`, `ANGEL_HIST_BURST`.

---

## Module 2 & 3 — Angel client + candles

### Unit tests (no network / no Angel account needed)

```bash
make deps          # Postgres + Redis (only Redis needed for the limiter test)
go test ./...
```

What's covered offline:
- `internal/candles` — **pure aggregation** tests (weekly/monthly OHLCV, confirmed-vs-forming flag, unsorted input, empty input).
- `internal/angel` — **httptest-based** tests for login (TOTP body + identity headers) and historical-candle parsing. No real Angel calls.
- `internal/ratelimit` — token-bucket behavior (needs Redis).

### Config for live Angel calls

To exercise the endpoints that actually hit Angel (`/angel/login`, `/scripmaster/sync`,
`/historical`, `/candles/sync`), fill these in `.env`:

```
ANGEL_API_KEY=your_smartapi_key
ANGEL_CLIENT_CODE=your_client_code
ANGEL_PIN=your_pin
ANGEL_TOTP_SECRET=your_base32_totp_secret
```

`instruments/search` and `candles?tf=...` (reads) work without Angel once data is loaded.

### Endpoints, in the order you'd test them

Import the updated [`postman_collection.json`](./postman_collection.json). Typical flow:

**1) Log in to Angel**
```
POST {{baseUrl}}/v1/angel/login
```
No body. Returns a non-secret token snapshot:
```json
{"logged_in":true,"jwt_len":812,"feed_token_len":220,"refresh_token_len":180,"acquired_at":"..."}
```
Check anytime with `GET /v1/angel/status`.

**2) Load the tradable universe (NSE + BSE equities)**
```
POST {{baseUrl}}/v1/angel/scripmaster/sync
```
No body. Downloads the Angel scrip master and upserts **NSE** cash equities
(`-EQ` symbols) **and BSE** cash equities (exch_seg `BSE`, no `-EQ` suffix):
```json
{"fetched":6200,"upserted":6200}
```
The same stock can appear on both exchanges (e.g. `RELIANCE-EQ` on NSE and
`RELIANCE` on BSE) — they're stored separately and the Watchlist shows an
**Exchange** column so you can tell them apart.

**3) Search instruments (watchlist autocomplete)**
```
GET {{baseUrl}}/v1/instruments/search?q=reli&limit=10
```
```json
{"count":2,"instruments":[{"id":1,"symbol_token":"2885","exchange":"NSE","trading_symbol":"RELIANCE-EQ","name":"RELIANCE","lot_size":1}]}
```
Grab the `id` for the next steps.

**4) Sync candle history for one instrument** (fetch daily + build weekly/monthly)
```
POST {{baseUrl}}/v1/instruments/1/candles/sync?days=1300
```
No body. `days` optional (default 1300, capped at 2000 = Angel's per-request limit).
```json
{"instrument_id":1,"trading_symbol":"RELIANCE-EQ","daily_fetched":1290,"daily_stored":1290,"weekly_candles":260,"monthly_candles":62}
```

**5) Read stored candles**
```
GET {{baseUrl}}/v1/instruments/1/candles?tf=1D&limit=5
GET {{baseUrl}}/v1/instruments/1/candles?tf=1W&limit=5
GET {{baseUrl}}/v1/instruments/1/candles?tf=1M&limit=5
```
Weekly/monthly show `is_confirmed` — the most recent bar is `false` (still forming),
which is exactly the bar the weekly/monthly scanners are allowed to fire on later.

**Optional — raw historical passthrough** (debugging, bypasses the store)
```
POST {{baseUrl}}/v1/angel/historical
Content-Type: application/json

{ "exchange": "NSE", "symbol_token": "2885", "from": "2024-01-01", "to": "2024-03-01" }
```
`from`/`to` optional (default last 30 days). Returns parsed candles directly from Angel.

### Notes
- Every `/historical` and `/candles/sync` call goes through the shared rate limiter, so
  bulk syncs self-throttle to `ANGEL_HIST_RATE`.
- Instruments must be loaded (step 2) before `candles/sync` — it looks up the symbol token by id.

---

## Modules 4, 5 & 6 — indicators, scanners, signals, scheduler

### Unit tests (all offline — no Angel, no Postgres needed)

```bash
go test ./...
```

Covered:
- `internal/indicators` — SMA/EMA/RSI/highest/lowest/crossover against known values.
- `internal/scanner` — weekly scanner predicates (fires / doesn't fire with RSI window,
  higher-low rule), `ScanWeekly` always reports 4 results, Pine produces no signal on flat
  data and is safe on short input, engine wires all timeframes.
- `internal/calendar` — trading-day detection, weekend/holiday skipping, missing-day gap
  detection (distinguishes real gaps from weekends).
- Plus everything from Modules 1-3.

### How scanning works (recap)

- **Pine "Chase Momentum"** runs on Daily, Weekly, Monthly → **binary BUY/SELL, no confidence**.
  Daily uses the closed bar; weekly/monthly may fire on the forming bar.
- **4 weekly Chartink scanners** run on weekly candles → **confidence = N/4** fired; a weekly
  signal is emitted when **N ≥ 1**.
- Every fired signal is written to the `signals` audit table (idempotent on
  `instrument+source+scanner+timeframe+candle_date`), and cleaned up after **30 days**.
- The **scheduler** runs the daily post-close scan (with reconciliation/backfill) and nightly
  cleanup; it also reconciles on startup. Configure via `DAILY_SCAN_CRON`, `CLEANUP_CRON`,
  `RETENTION_DAYS`, `SCHEDULER_ENABLED`, `RECONCILE_ON_STARTUP` in `.env`.

### Endpoints (continue after loading candles for an instrument)

**Run scanners on stored candles** (no Angel call; uses what's already synced)
```
POST {{baseUrl}}/v1/instruments/1/scan
```
```json
{
  "instrument_id": 1,
  "report": {
    "daily_pine":  {"buy": false, "sell": false, "reasons": { "...": false }},
    "weekly_pine": {"buy": false, "sell": false, "reasons": {}},
    "monthly_pine":{"buy": false, "sell": false, "reasons": {}},
    "weekly_scanners": {"confidence": 2, "fired": ["weekly_1","weekly_3"], "details": {"weekly_1":true,"weekly_2":false,"weekly_3":true,"weekly_4":false}}
  },
  "signals_inserted": 1
}
```

**Fetch from Angel then scan in one shot**
```
POST {{baseUrl}}/v1/instruments/1/sync-scan?days=1300
```

**Browse the signal audit log**
```
GET {{baseUrl}}/v1/signals?instrument_id=1&tf=1W&source=weekly&limit=50
```
All filters optional: `instrument_id`, `tf` (1D|1W|1M), `source` (pine|weekly), `limit`.

**Trading-calendar check**
```
GET {{baseUrl}}/v1/calendar/check?date=2026-01-26
```
```json
{"date":"2026-01-26","is_trading_day":false,"weekday":"Monday"}
```
(Returns `true` for a normal weekday until you add it as a holiday.)

**Add exchange holidays** (weekends are automatic; add the rest here)
```
POST {{baseUrl}}/v1/admin/holidays
Content-Type: application/json

{ "dates": ["2026-01-26", "2026-03-06"] }
```

**Admin / ops**
```
POST {{baseUrl}}/v1/admin/reconcile?id=1     # reconcile one instrument (id optional → all)
POST {{baseUrl}}/v1/admin/reconcile          # reconcile ALL tracked instruments
POST {{baseUrl}}/v1/admin/scan-all           # scan all tracked (stored candles only)
POST {{baseUrl}}/v1/admin/cleanup            # delete signals older than RETENTION_DAYS now
```

`reconcile` detects missing trading days (ignoring weekends/holidays), backfills only the
gaps from Angel, rebuilds weekly/monthly, and re-scans — this is what runs on startup and
after any downtime.

### Full endpoint reference

| Method | Path | Purpose |
|---|---|---|
| GET  | `/health` | liveness |
| GET  | `/health/ready` | readiness (PG + Redis) |
| POST | `/v1/ratelimit/try` | rate-limiter demo |
| POST | `/v1/angel/login` | Angel auth |
| GET  | `/v1/angel/status` | token snapshot |
| POST | `/v1/angel/scripmaster/sync` | load NSE-EQ instruments |
| POST | `/v1/angel/historical` | raw candle passthrough |
| GET  | `/v1/instruments/search?q=` | autocomplete |
| GET  | `/v1/instruments/{id}` | instrument detail |
| POST | `/v1/instruments/{id}/candles/sync?days=` | fetch daily + build W/M |
| GET  | `/v1/instruments/{id}/candles?tf=1D\|1W\|1M` | read candles |
| POST | `/v1/instruments/{id}/scan` | scan stored candles |
| POST | `/v1/instruments/{id}/sync-scan?days=` | fetch then scan |
| GET  | `/v1/signals?instrument_id=&tf=&source=&limit=` | audit browse |
| GET  | `/v1/calendar/check?date=` | trading-day check |
| POST | `/v1/admin/holidays` | add holidays |
| POST | `/v1/admin/reconcile?id=` | reconcile one/all |
| POST | `/v1/admin/scan-all` | scan all tracked |
| POST | `/v1/admin/cleanup` | retention cleanup now |
| POST | `/v1/users` | create user |
| GET  | `/v1/users` | list users |
| POST | `/v1/users/{uid}/watchlists` | create watchlist |
| GET  | `/v1/users/{uid}/watchlists` | list watchlists |
| POST | `/v1/watchlists/{wid}/items` | add instrument to watchlist |
| DELETE | `/v1/watchlists/{wid}/items/{instrumentId}` | remove instrument |
| PUT  | `/v1/users/{uid}/scanner-prefs` | set enabled scanners |
| GET  | `/v1/users/{uid}/scanner-prefs` | get scanner prefs |
| PUT  | `/v1/users/{uid}/telegram` | set Telegram bot/chat |
| GET  | `/v1/users/{uid}/telegram` | get Telegram config |
| POST | `/v1/telegram/test` | send a connectivity test message |
| GET  | `/v1/signals/{id}/recipients` | preview fan-out recipients |
| POST | `/v1/admin/dispatch?signal_id=` | manually dispatch a signal |
| GET  | `/v1/analytics/summary` | aggregated signal stats |
| GET  | `/v1/analytics/export.xlsx` | Excel export |

---

## Modules 7 & 8 — notifications, fan-out, analytics, Excel

### How fan-out + notification rules work

- Scanners run **once per stock** (platform-wide). When a signal is generated it's
  distributed only to users who **watch that stock** AND have that **scanner enabled**
  AND have **Telegram enabled**.
- **7-day send window** (`NOTIFY_WINDOW_DAYS`): a signal whose candle date is older than
  7 days is stored in the audit log but **not sent**. So after the server is down for a
  while, reconciliation backfills the data and re-scans, but only *fresh* signals go out.
- **Dedup:** the same **stock + timeframe + day** is never sent twice to a user
  (enforced by a UNIQUE constraint on `signal_deliveries`). **Different timeframes** for
  the same stock are separate notifications and DO get sent (e.g. a daily and a weekly
  signal on the same stock both go out).
- Retention cleanup (30 days) is independent of the 7-day send window.
- **Safety net (default chat):** set `TELEGRAM_DEFAULT_BOT_TOKEN` + `TELEGRAM_DEFAULT_CHAT_ID`
  in `.env` and that chat receives **every in-window signal once** (deduped per
  stock+timeframe+day). This catches signals for users who haven't configured their own
  bot *and* signals for stocks nobody watches — so nothing is ever lost. Users who set
  their own bot still get personal delivery on top. Leave both blank to disable.

### Quick "is my bot connected?" check

```
POST {{baseUrl}}/v1/telegram/test
{}                                  # -> sends to the env default/safety-net chat
{ "user_id": "<uuid>" }             # -> sends via that user's saved bot/chat
{ "bot_token": "123:ABC", "chat_id": "999" }   # -> ad-hoc, no saving
```
Returns `{ "sent": true }` and a message lands in the chat. This bypasses the
signal pipeline (no 7-day window, no dedup) — purely to verify the token + chat id.

### Testing without a real Telegram bot

Notifications need a per-user bot token + chat id. Two options:
1. **Real:** create a bot via @BotFather, get your chat id, and set them via
   `PUT /v1/users/{uid}/telegram`.
2. **Mock:** point `TELEGRAM_BASE_URL` in `.env` at a local mock that returns
   `{"ok":true}` for `/bot.../sendMessage`. Then use `GET /v1/signals/{id}/recipients`
   and `POST /v1/admin/dispatch` to exercise the fan-out logic without a real bot.

### End-to-end flow (Postman order)

```
# 1. Create a user
POST {{baseUrl}}/v1/users
{ "email": "krish@example.com" }
# → { "id": "<uuid>", ... }   (save as {{userId}})

# 2. Create a watchlist for the user
POST {{baseUrl}}/v1/users/{{userId}}/watchlists
{ "name": "Momentum" }
# → { "id": "<uuid>", ... }   (save as {{watchlistId}})

# 3. Add an instrument (use an id from /instruments/search)
POST {{baseUrl}}/v1/watchlists/{{watchlistId}}/items
{ "instrument_id": 1384 }

# 4. Enable the scanners this user cares about
PUT {{baseUrl}}/v1/users/{{userId}}/scanner-prefs
{ "prefs": { "pine_1d": true, "pine_1w": true, "weekly_1": true, "weekly_2": true, "weekly_3": true, "weekly_4": true } }

# 5. Configure Telegram (real bot, or a mock via TELEGRAM_BASE_URL)
PUT {{baseUrl}}/v1/users/{{userId}}/telegram
{ "bot_token": "123:ABC", "chat_id": "99999", "enabled": true }

# 6. Run a scan — any fresh signal auto-fans-out to matching users
POST {{baseUrl}}/v1/instruments/1384/scan

# 7. Inspect who a signal would reach (grab an id from /v1/signals)
GET  {{baseUrl}}/v1/signals?instrument_id=1384
GET  {{baseUrl}}/v1/signals/{signalId}/recipients

# 8. Manually (re)dispatch a signal to test the window + dedup
POST {{baseUrl}}/v1/admin/dispatch?signal_id={signalId}
# → { "sent": 1, "skipped_duplicate": 0, "recipients": 1, "dropped": false }
# run it again → { "sent": 0, "skipped_duplicate": 1, ... }  (dedup working)
```

Scanner-pref keys: `pine_1d`, `pine_1w`, `pine_1m`, `weekly_1`, `weekly_2`, `weekly_3`, `weekly_4`.

### Analytics + Excel

```
GET {{baseUrl}}/v1/analytics/summary?from=2026-01-01&to=2026-12-31&tf=1W&source=weekly
```
```json
{
  "total": 12,
  "by_timeframe": {"1D": 4, "1W": 8},
  "by_source": {"pine": 5, "weekly": 7},
  "by_direction": {"BUY": 12},
  "by_scanner": {"pine": 5, "weekly_1,weekly_3": 4, "weekly_1": 3},
  "confidence_distribution": {"1": 3, "2": 4}
}
```

Excel export (same filters) — returns an `.xlsx` with a **Signals** sheet and a
**Summary** sheet:
```
GET {{baseUrl}}/v1/analytics/export.xlsx?from=2026-01-01&to=2026-12-31
```
In Postman use **Send and Download** to save the file.

---

## Modules 9 & 10 — paper trading + the web app

### Backend: paper trading & market data

- **Market-aware execution:** `Buy` executes immediately at the current price when the
  market is open (NSE 09:15–15:30 IST, trading day), otherwise the trade is `SCHEDULED`
  and filled at the next open by the scheduler (`FILL_SCHEDULED_CRON`, default 09:16 IST).
- **Price source:** live Angel LTP when available, else the last stored daily close.
- **Trades are only allowed on stocks with a valid BUY signal** (`signal_id` is required
  and validated).
- **Live price + indicators** for the dashboard come from `/v1/instruments/{id}/params`
  and `/v1/users/{uid}/dashboard`. **Trending** is `/v1/market/trending`.

New endpoints:

| Method | Path | Purpose |
|---|---|---|
| GET  | `/v1/market/trending?limit=` | top daily % gainers |
| GET  | `/v1/instruments/{id}/params` | latest indicators + live price |
| GET  | `/v1/instruments/{id}/coverage` | what's stored for a stock (candle counts, date range, gaps) |
| GET  | `/v1/users/{uid}/dashboard` | params for all watchlist stocks |
| GET  | `/v1/users/{uid}/coverage` | storage/coverage for all watchlist stocks |
| PUT  | `/v1/users/{uid}/paper/capital` | set virtual capital |
| GET  | `/v1/users/{uid}/paper/account` | account (capital, cash) |
| POST | `/v1/users/{uid}/paper/trades` | buy `{signal_id, quantity}` |
| GET  | `/v1/users/{uid}/paper/trades` | trade history + open positions |
| GET  | `/v1/users/{uid}/paper/summary` | invested / P&L / booked profit-loss |
| POST | `/v1/paper/trades/{tradeId}/close` | close a position |

### Frontend: run the web app

The SPA lives in `web/` (Vite + React, no UI framework, dark/light). It talks to the Go
server via a dev proxy — no CORS setup needed.

```bash
# Terminal 1 — backend (from repo root)
make deps          # Postgres + Redis
make tidy && make run   # server on :8080 (applies migrations 0001–0006)

# Terminal 2 — frontend
cd web
npm install
npm run dev        # opens http://localhost:5173
```

`/v1` and `/health` are proxied to `:8080` (see `web/vite.config.js`).

### Using the app (end-to-end, no Postman)

1. **Top-right:** click **+ User**, enter an email — it's created and selected (persisted
   in the browser). Toggle the switch for **dark/light**.
2. **Home** — trending stocks (highest daily % up). Needs some instruments synced first.
3. To populate data, the quickest path is still one-time setup calls (or the Postman
   collection): `POST /v1/angel/login` → `POST /v1/angel/scripmaster/sync` →
   `POST /v1/instruments/{id}/candles/sync?days=1300` for a few stocks, then add those
   instruments to the user's watchlist (`/v1/watchlists/...`) and run `/v1/admin/scan-all`.
   After that the whole UI is populated.
4. **Analytics** — every watchlist stock with live price + RSI/EMA/SMA/ATR/volume;
   **Export CSV** (opens in Excel) or export signals as `.xlsx`.
5. **Scanner → Pine / Weekly** — current + last-7-days signals; enter a qty and **Buy**
   (market-aware paper trade). Weekly shows the `N/4` confidence.
6. **Audit** — all signals (retained 30 days, then auto-removed from DB + UI), filterable
   by source/timeframe.
7. **Paper Trading** — invested, market value, unrealized/total P&L, positions with a
   **Close** button.
8. **Profile** — set virtual **capital**; see total P&L, realized P&L, **booked profit**
   and **booked loss**, cash, equity, open positions.

> Note: live LTP requires valid `ANGEL_*` creds and a logged-in session (`/v1/angel/login`).
> Without them, prices fall back to the last stored daily close so the app still works.

### Building the frontend for production

```bash
cd web && npm run build   # outputs web/dist (static files you can serve anywhere)
```

---

## Authentication (Module 11)

All `/v1/*` endpoints (except `/v1/auth/*`) now require a **JWT**. `/health` stays public.

```
POST /v1/auth/register  { "email": "you@x.com", "password": "min6chars" }
POST /v1/auth/login     { "email": "you@x.com", "password": "..." }
# → { "token": "<jwt>", "user": { "id": "...", "email": "..." } }
```

Send it as `Authorization: Bearer <jwt>` on every other call. Set `JWT_SECRET` in `.env`.

- **In the web app:** you get a proper **login/register screen**; the token is stored and
  attached automatically, and a 401 bounces you back to login. Sign out from the sidebar.
- **In Postman/curl:** log in once, copy the `token`, and add the `Authorization` header
  (the older collection requests need this header added now).

## Using the web app (updated flow)

1. **Register / log in** on the landing screen.
2. **Watchlist** — search NSE stocks and **Add**; adding also **fetches history**
   (`candles/sync`) so scanners have data. Remove stocks anytime.
3. **Home** — trending movers (gradient hero).
4. **Analytics** — watchlist stocks with live price + RSI/EMA/SMA/ATR/volume; CSV export.
5. **Scanner → Pine / Weekly** — current + last-7-day signals; **Run scan now** to generate
   fresh ones on demand; enter qty and **Buy** (paper).
6. **Audit** — all signals (30-day retention).
7. **Paper Trading** — P&L cards + positions with **Close**.
8. **Profile** — set capital; **configure Telegram** (bot token + chat id) with a
   **Send test** button; view total/realized P&L, booked profit & loss.

## Telegram: fixing "chat not found"

That error is Telegram rejecting the `chat_id` (the message left the server fine):

1. **DM your bot first** — open the bot in Telegram and press **Start**. A bot can't message
   a user who has never contacted it.
2. **Get the real chat id** — send your bot a message, then open
   `https://api.telegram.org/bot<token>/getUpdates` and copy the numeric `chat.id`.
3. **For a channel** — add the bot as an **admin**, and use the `-100…` chat id (or `@name`).
4. Verify instantly with **Profile → Send test**, or `POST /v1/telegram/test { "user_id": "…" }`.

## Project audit — what works vs. known gaps

**Working end-to-end:** auth, watchlist + history sync, candle aggregation, indicators,
Pine + weekly scanners, signals audit + 30-day cleanup, reconciliation/backfill on
startup, scheduler, Telegram fan-out + 7-day window + safety-net chat, analytics + Excel,
paper trading (market-aware), and the full web UI.

**Deliberately not built yet (future work):**
- **Live WebSocket prices** — the UI/paper engine use Angel LTP (REST, best-effort) and
  fall back to last daily close. No streaming ticks.
- **Interactive Telegram buy button** — alerts are outbound only; buying happens in the web
  app. (Inline keyboard + callback handling would be a follow-up.)
- **Per-endpoint ownership checks** — any valid token can pass a `{uid}` in the path; fine
  for local single-tenant use, but add an "is this my uid" guard before multi-tenant use.
- **`DAILY_SCAN_CRON` default is 15:20 IST** (from the "~3:15 PM" spec) which is *before*
  the 15:30 close; set it to ~15:45 or later so the finalized daily candle is available.


add algo-trading and real money account connect so that user can do real algo trading

add big investor in 360 degree stock as well