# TradeNexus — Implementation Plan

**Version:** 1.0 (planning)
**Stack:** Go (backend) · PostgreSQL (persistence) · Redis (cache/real-time) · React (frontend)
**Broker:** Angel One SmartAPI (Historical REST + SmartStream WebSocket 2.0)
**Notifications:** Telegram Bot API
**Date:** July 2026

---

## 1. Scope & key decisions

These are locked from requirements clarification and drive the whole design.

| Area | Decision |
|---|---|
| Scanner set A — **Pine Script** ("Chase Momentum Pro Clean") | Ported to **3 timeframes: Daily (1D), Weekly (1W), Monthly (1M)**. Produces a **binary** Buy/Sell signal. **No confidence score** — it either fires or it doesn't. |
| Scanner set B — **Weekly Chartink scanners** | 4 independent scanners run on **weekly candles**. Confidence = **count of the 4 that fire (0–4)**, shown as `N/4`. |
| Source data | Angel One Historical API delivers **daily candles only** (max **2000 candles/request**). Weekly & monthly candles are **derived internally** by aggregating daily OHLCV. |
| Candle confirmation | **Two-tier rule.** **Daily** scanners fire **only on a closed daily candle**. **Weekly & monthly** scanners are allowed to fire on the **current partial (forming) candle** — rebuilt each day from *confirmed daily bars* — so a valid signal is dispatched early without waiting for the week/month to close. |
| Valid outcome | Producing **zero signals** for a stock is normal and correct. The engine must never fabricate a signal to "have" one. |
| Paper trading | Allowed only on stocks that produced a **valid scanner signal**. Buy signals are tradeable. |
| **Compute once, fan out** | Each scanner runs **once per stock for the whole platform** — never per user. Generated signals are then **distributed only to users** who (a) have that stock in a watchlist **and** (b) have that scanner enabled. Scales to thousands of concurrent users. |
| Tracked universe | The set of stocks scanned = the **union of all stocks across all users' watchlists** (deduplicated). No stock is scanned unless at least one user watches it. |
| Weekly scanner threshold | A weekly signal fires when **N ≥ 1** of the 4 scanners is true (confidence still shown as `N/4`). |
| Signal retention | Signals are cleaned up **every 30 days** (older than 30 days removed from Postgres + Redis). |
| Deployment | **Multi-user**, run **locally** for now (Docker Compose on the dev machine). Cloud/K8s deferred. |
| Watchlist add | User types a name; an **Angel-One-backed autocomplete dropdown** shows valid stocks; on add, the platform fetches the required history for that stock (sized to the deepest lookback in the strategies). |

**Terminology:** a "signal event" = one scanner firing on one stock on one timeframe on one confirmed candle. Pine emits Daily/Weekly/Monthly signal events; the 4 weekly scanners emit weekly signal events that are aggregated into a single `N/4` confidence for that stock/week.

---

## 2. Architecture overview

Modular monolith in Go (single deployable, internally decoupled packages) — simplest path to "production-ready" for this scope, and cleanly splittable into services later.

```
                         ┌──────────────────────────────────────┐
  Angel One SmartAPI ───►│  Ingestion & Sync Engine              │
   (Historical REST,     │  · auth/session (TOTP+JWT)            │
    SmartStream WS 2.0)   │  · scrip master loader               │
                         │  · daily candle fetch + backfill     │
                         │  · reconciliation on startup/recovery │
                         └──────────────┬───────────────────────┘
                                        ▼
                         ┌──────────────────────────────────────┐
                         │  Candle store (Postgres)              │
                         │  daily → derive weekly / monthly      │
                         └──────────────┬───────────────────────┘
                                        ▼
                         ┌──────────────────────────────────────┐
                         │  Indicator engine (EMA/SMA/RSI/ATR…)  │
                         │  incremental + full-replay modes      │
                         └──────────────┬───────────────────────┘
                                        ▼
        ┌───────────────────────────────────────────────────────┐
        │  Scanner engine (concurrent worker pool)               │
        │  A) Pine port  → D / W / M   (binary)                  │
        │  B) 4 weekly Chartink scanners → weekly (N/4 conf.)    │
        └───────┬───────────────────────────────┬───────────────┘
                ▼                                ▼
     ┌───────────────────┐            ┌──────────────────────────┐
     │ Signal + Audit     │            │ Paper trading engine     │
     │ store (Postgres)   │            │ market-aware execution   │
     │ + Redis cache      │            └──────────┬───────────────┘
     └────┬──────────┬────┘                       │
          ▼          ▼                            ▼
   ┌───────────┐  ┌──────────────┐        ┌──────────────┐
   │ Telegram  │  │ Analytics +  │        │ React SPA    │
   │ bot (out  │  │ Excel export │◄──────►│ (dashboard,  │
   │ + inbound │  │ (REST API)   │        │  watchlists) │
   │ commands) │  └──────────────┘        └──────────────┘
   └───────────┘
                     ▲
          ┌──────────┴──────────┐
          │ Scheduler (cron)    │  daily close, EOD sync, retention cleanup
          └─────────────────────┘
```

### 2.1 Go package layout

```
/cmd/server           main entrypoint, wiring, config
/cmd/worker           (optional) separate scanner worker binary
/internal/config      env/config loading
/internal/angel       SmartAPI client: auth, historical, websocket, scrip master
/internal/calendar    NSE trading calendar + holiday list
/internal/candles     daily store + weekly/monthly aggregation
/internal/indicators  EMA, SMA, RSI, ATR, highest/lowest, crossover helpers
/internal/scanner     pine engine + weekly scanners + orchestrator
/internal/signals     signal model, audit store, retention
/internal/notify      telegram bot (outbound + command handling)
/internal/paper       paper trading engine, portfolio, P&L
/internal/analytics   dashboard aggregation + excel export (excelize)
/internal/sync        sync engine + reconciliation
/internal/scheduler   cron jobs
/internal/store       postgres (pgx) + redis (go-redis) repositories
/internal/api         REST handlers, auth middleware, websocket push to UI
/web                  React app
```

### 2.2 Recommended libraries

Postgres `jackc/pgx/v5`; migrations `golang-migrate`; Redis `redis/go-redis/v9`; cron `robfig/cron/v3`; Excel `xuri/excelize/v2`; Telegram `go-telegram-bot-api` (or raw HTTP); HTTP router `chi`; config `caarlos0/env` + `.env`; TOTP `pquerna/otp`; logging `zerolog`.

---

## 3. Angel One SmartAPI integration

### 3.1 Authentication / session
- Login with API key + client code + PIN/password + **TOTP** → returns `jwtToken`, `refreshToken`, `feedToken`.
- Store tokens in Redis with TTL; refresh via `refreshToken` before expiry; re-login with fresh TOTP if refresh fails.
- **All historical calls** send `Authorization: Bearer <jwtToken>` plus the mandatory client-identity headers (local IP, public IP, MAC, `X-PrivateKey`).

### 3.2 Scrip / instrument master
- Download the full instrument dump (OpenAPIScripMaster JSON) daily; upsert into `instruments` table.
- Maps tradingsymbol ⇄ `symboltoken` ⇄ `exchange` — required for every historical request.

### 3.3 Historical Candle Data
- Endpoint: `POST /rest/secure/angelbroking/historical/v1/getCandleData`.
- Body: `{ exchange, symboltoken, interval, fromdate, todate }`, dates `YYYY-MM-DD HH:mm`.
- Interval used: **`ONE_DAY`** (we derive everything higher). Per-request cap ≈ **2000 daily candles** → one call fetches ~8 trading years; enough for weekly EMA(200) (~4 yrs) and monthly indicators.
- Response: array of `[timestamp, open, high, low, close, volume]` in IST.
- **Rate limits:** respect SmartAPI limits (historical is the tightest bucket). Implement a **Redis token-bucket limiter** (configurable, default conservative ≈ 3 req/s and a per-minute cap) with exponential backoff + jitter on `429`/throttle. Batch stocks through a bounded fetch worker pool.

### 3.4 Live prices (WebSocket 2.0 / SmartStream)
- Subscribe with `feedToken` for LTP/quote of watchlist tokens; push to Redis + fan out to React over the app's own WS.
- **Important:** live ticks are for display only. **Scanners never run on live/intraday data** — only on confirmed daily/weekly/monthly candles from the historical store.

### 3.5 Watchlist symbol search + history bootstrap
- **Autocomplete search:** the scrip master (Section 3.2) is indexed (Postgres trigram / Redis) so a `GET /instruments/search?q=` returns matching valid Angel-One tradable symbols as the user types. Users can **only add stocks that exist in the Angel scrip master** — no free-text stocks.
- **On add → history bootstrap:** when a stock is added to *any* user's watchlist for the first time, the platform:
  1. Computes the **required lookback** = the deepest indicator window across all strategies, per timeframe, converted to daily bars:
     - Weekly EMA(200) ⇒ 200 weekly bars ≈ **200 × 5 = 1000** daily bars (biggest weekly need).
     - Monthly EMA(50)/SMA(40) ⇒ ~50 monthly bars ≈ **50 × 21 ≈ 1050** daily bars (biggest monthly need).
     - Daily Pine (max window ~50) is trivial by comparison.
     - `required_daily = max(weekly_need, monthly_need) + warmup_buffer` (EMA seeded from SMA to keep the buffer small).
  2. This lands **≈ 1,100–1,300 daily candles** — comfortably within the **2000-candle single-request cap**, so it's normally **one** historical call. If a future strategy needs >2000, the fetcher paginates.
  3. Fetches exactly that many daily candles, stores them, builds weekly/monthly, computes indicators, and marks the stock ready to scan.
- **Shared, not duplicated:** history is fetched **once per stock** (not per user). A second user adding the same stock reuses the existing candle history — only their watchlist membership + scanner prefs are added.

---

## 4. Data model (PostgreSQL)

Core tables (columns abbreviated):

- `users` — id, email, auth, virtual_capital settings.
- `instruments` — symboltoken, exchange, tradingsymbol, name, lot_size, active.
- `watchlists` / `watchlist_items` — user-owned lists of instruments.
- `daily_candles` — instrument_id, trade_date (PK w/ instrument_id), o,h,l,c,v. **Source of truth.**
- `weekly_candles`, `monthly_candles` — derived, materialized for speed. period_start, period_end, o,h,l,c,v, `is_confirmed`.
- `indicators_daily/weekly/monthly` — per candle: ema10/20/50/200, sma40, rsi14, atr14, vol_sma20, highest_n, lowest_n, plus Pine state (`long_active`, `short_active`, `last_buy_bar`, `last_sell_bar`). Materializing state makes reconciliation deterministic.
- `signals` — **audit core**: id, instrument_id, source (`pine`|`weekly_scanner`), scanner_name, timeframe (`1D`|`1W`|`1M`), direction (`BUY`|`SELL`), candle_date, confidence (nullable; only weekly N/4), reason_json (which conditions passed), created_at.
- `scanner_runs` — run_id, timeframe, started_at, finished_at, stocks_scanned, signals_generated (observability).
- `paper_accounts` — user_id, starting_capital, cash_balance, equity.
- `paper_trades` — account_id, instrument_id, signal_id, side, qty, entry_price, entry_time, exit_price, exit_time, status (`OPEN`|`CLOSED`|`SCHEDULED`), pnl, source (`web`|`telegram`).
- `telegram_configs` — user_id, bot_token, chat_id, enabled, filters.
- `user_scanner_prefs` — user_id, scanner_key (`pine_1d`|`pine_1w`|`pine_1m`|`weekly_1`..`weekly_4`), enabled. Drives the fan-out: a user only receives a signal if the scanner is enabled here.
- `signal_deliveries` — signal_id, user_id, delivered_at, channel (`web`|`telegram`), seen. The join between a **platform-wide signal** and the **users it was distributed to** (also powers per-user dedupe and unread counts).
- `sync_state` — instrument_id, last_synced_date, last_full_reconcile_at.
- `market_holidays` — exchange, holiday_date (seeded + annually updated).

Indexes on `(instrument_id, trade_date)`, `(instrument_id, timeframe, candle_date)`, `signals(created_at)` for retention sweeps.

---

## 5. Candle aggregation (daily → weekly / monthly)

All timestamps in `Asia/Kolkata`. Aggregation rules per group:
- **open** = open of first trading day in the period
- **high** = max(high), **low** = min(low)
- **close** = close of last trading day in the period
- **volume** = sum(volume)

**Weekly grouping:** ISO week (Mon–Fri NSE session). A week is **confirmed** once its last trading day (usually Friday, or the last non-holiday weekday) has a stored daily candle.

**Monthly grouping:** calendar month; **confirmed** once the last trading day of the month is stored.

**Forming candle handling (two-tier):**
- The current in-progress week/month is stored with `is_confirmed=false` and **rebuilt every day** from the latest *confirmed daily* candles.
- **Daily timeframe:** only the **closed** daily candle is ever scanned — the forming daily bar is never evaluated.
- **Weekly & monthly timeframes:** the **forming candle is eligible for scanning**. Each trading day, after the daily candle is finalized, the current weekly/monthly candle is reconstructed and scanned; if conditions already hold, the signal fires immediately (early) rather than waiting for the period to close.
- The daily bars *inside* a forming weekly/monthly candle are always confirmed — so we scan a partial *period* built only from finalized *daily* data (no intraday/live data ever enters a scan).

**Intra-period dedupe:** because a forming weekly/monthly candle is re-scanned daily, a fired signal is keyed on `(stock, timeframe, period_start, scanner)` so the same week/month fires **at most once** even if the condition keeps holding on later days. (Optionally re-notify if confidence *increases*, e.g. weekly N/4 goes 2/4 → 3/4 — configurable.)

This satisfies the brief's "reconstruct the current weekly and monthly candles using the latest confirmed daily data … generate the Buy signal and immediately dispatch without waiting for any additional processing cycle."

---

## 6. Indicator engine

Implements Pine `ta.*` semantics on Go slices (chronological, index 0 = oldest):

- `ema(period)`, `sma(period)`, `rsi(period)` (Wilder), `atr(period)` (Wilder), `highest(n)`, `lowest(n)`.
- Series offset `[k]` = value k bars back.
- `crossover(a,b)` = `a[0]>b[0] && a[1]<=b[1]`; `crossunder` symmetric.
- Two modes:
  - **Full replay** — recompute the entire series from candle 0. Used on first load, backfill, and reconciliation. Required for the Pine **stateful** logic (`longActive`, cooldown by `bar_index`) which must be replayed deterministically so a mid-history recovery produces identical state to an uninterrupted run.
  - **Incremental append** — O(1) update when a single new confirmed candle arrives (steady-state daily close).

Weekly indicators need enough history: EMA(200) weekly ⇒ ~200 weekly candles ⇒ ~4 years of daily data. The 2000-candle daily fetch covers this comfortably.

---

## 7. Scanner engine

Orchestrator fans stocks across a **bounded worker pool** (concurrent scanning), each worker pulls confirmed candles + indicators for a stock/timeframe and evaluates scanners. Results written to `signals`, cached in Redis, and dispatched to notify/paper.

### 7.1 Scanner A — Pine "Chase Momentum Pro Clean" (D / W / M, binary)

Direct translation of the provided Pine v6. **Daily** runs on closed daily candles only; **Weekly/Monthly** run on the current forming candle (rebuilt daily from confirmed daily bars) so signals fire early:

- **Moving averages:** ema10, ema20, sma40, ema50.
- **bullTrend** = `ema10>ema20 && ema20>sma40 && close>ema10 && sma40>sma40[1]` (bearTrend symmetric).
- **Breakout:** `highestLevel = highest(high,20)[1]`, `freshBullBreakout = crossover(close, highestLevel)` (bear = crossunder low).
- **Volume:** `avgVolume=sma(volume,20)`, `volumeSpike = volume > avgVolume*1.8`.
- **Strong candle:** `bodySize=|close-open|`, `atr=atr(14)`, `strongBullCandle = close>open && bodySize>atr*0.5`.
- **Momentum:** `rsi=rsi(14)`, `bullMomentum = rsi>60` (bear `rsi<40`).
- **State machine (replayed over history):** `longActive`/`shortActive` with resets (`close<ema10` or `crossunder(ema10,ema20)`), and **cooldown** of `12` bars via `bar_index - lastBuyBar > 12`.
- **buySignal** = `bullTrend && freshBullBreakout && volumeSpike && strongBullCandle && bullMomentum && !longActive && canBuy`. sellSignal symmetric.
- Emits a `signals` row with `source=pine`, the timeframe, direction, **no confidence**, and `reason_json` capturing each sub-condition.

Note on inputs: cooldown=12, breakoutLength=20, volumeMultiplier=1.8 are made **configurable per timeframe** (defaults from the script) so tuning is possible later.

### 7.2 Scanner B — 4 weekly Chartink scanners (weekly, N/4 confidence)

Each runs on the weekly series where the **latest bar may be the forming (partial) week** rebuilt from confirmed daily bars; prior weeks (`[1]`, `[2]`, `[4]`, 52-wk lookbacks) are always closed weeks. `weekly(...)`/`latest`/`[k]` map to weekly-series ops. Confidence for a stock/week = **number of these that return true (0–4)**. A stock is a weekly candidate if `N ≥ 1` (threshold configurable).

**Scanner 1 — Weekly breakout**
`close > max(close[1..52])` (52-wk high, prior weeks) · `volume > ema(20,volume)` · `close > ema(20)` · `ema20 > ema50 > ema200` · `50 < rsi(14) < 75` · `close ≥ open`.

**Scanner 2 — Weekly continuation**
`close > close[1]` · `close > ema20` · `ema20 > ema50 > ema200` · `low ≥ low[1]` · `close > high[1]` · `volume ≥ volume[1]` · `50 < rsi(14) < 70`.

**Scanner 3 — Weekly 52-wk high breakout (no-EMA structure)**
`close > max(high[1..52])` · `volume > max(volume[1..4])` · `close ≥ open` · `high > high[1]` · `low > low[4]` · `50 < rsi(14) < 75`.

**Scanner 4 — Weekly continuation (pure price action)**
`close > high[1]` · `low ≥ low[1]` · `high > high[1]` · `low > low[4]` · `close ≥ open` · `volume ≥ volume[1]` · `50 < rsi(14) < 70` · `close > close[1]` · `close[1] > close[2]`.

Output: one weekly aggregate signal per stock storing `confidence=N`, `reason_json` listing **which of the 4 fired**, so dashboard/Telegram can show scanner names + timeframe.

### 7.3 What a dispatched signal carries
`stock`, `source` (Pine vs weekly), `scanner name(s)`, `timeframe` (1D/1W/1M), `direction`, `confidence` (`N/4` for weekly, none for Pine), candle date, and whether it's paper-tradeable (BUY).

### 7.4 Compute-once, fan-out distribution (scalability)

**The scanner never runs per user.** Computation and distribution are fully decoupled:

1. **Compute (once per stock):** the engine scans each distinct stock in the tracked universe (= union of all watchlists) a single time per cycle and writes **one platform-wide `signals` row** per signal event. This cost is independent of how many users watch the stock — 1 user or 10,000 users, the scan runs once.
2. **Fan-out (cheap DB join):** when a signal is written, the distributor resolves recipients with a single query —
   `users who have {stock} in a watchlist ∩ users with {scanner_key} enabled in user_scanner_prefs` — and inserts `signal_deliveries` rows + enqueues Telegram/web pushes only for those users.
3. **Result:** adding users adds only lightweight watchlist/pref rows and fan-out inserts; it does **not** multiply scanning work. This is what keeps the platform scalable under thousands of concurrent users.

Redis pub/sub broadcasts each new signal; per-user web sockets filter to their deliveries. Telegram sends respect each recipient's `telegram_configs` + filters.

---

## 8. Scheduler & timing (IST)

- **Daily close job (~15:15–post close):** after the daily candle is finalized, (1) fetch/confirm today's daily candle, (2) rebuild forming & newly-confirmed weekly/monthly candles, (3) recompute indicators, (4) run **Daily Pine** scanner, and (5) run **Weekly + Monthly** validation — reconstruct the **current forming** weekly/monthly candle from confirmed daily bars and, if it already satisfies conditions, emit the signal **immediately** (early, without waiting for the week/month to close), deduped per `(stock, timeframe, period_start, scanner)`. *Note: NSE regular close is 15:30; the "~15:15" in the brief is treated as a configurable trigger — the job also guards on "daily candle present" before running.*
- **Weekly signals fire intra-week** on the forming candle (daily re-scan). On the last trading day of the week the candle flips confirmed and gets a final evaluation — any not-yet-sent signal is emitted; already-sent ones are suppressed by the period dedupe key.
- **Monthly signals** work the same way: fired early on the forming monthly candle, with a final confirmed-candle pass on the last trading day of the month.
- **Retention cleanup job (daily):** delete `signals` (and their `signal_deliveries`) **older than 30 days** from **both Postgres and Redis**.
- **Scrip master refresh:** daily pre-open.
- **Paper trade scheduler:** at next market open, execute any `SCHEDULED` paper trades.

---

## 9. Resilience & reconciliation (fault tolerance)

The reconciliation module is the backbone of "no missed opportunity."

**On every startup / restart / recovery:**
1. For each tracked instrument, compare stored `daily_candles` against the **NSE trading calendar** (weekdays minus `market_holidays`) between `last_synced_date` and today.
2. Classify each absent day: **legit non-trading** (weekend/holiday → ignore) vs **actual gap** (should have data → backfill).
3. For real gaps, fetch **only** the missing daily candles (batched, rate-limited).
4. Rebuild affected weekly/monthly candles, **recompute indicators via full replay** (restores correct Pine state), and **re-run D/W/M scanners** over the affected range.
5. Emit any signals that should have fired during the outage, dispatch to Telegram, dedupe against already-sent signals (Redis idempotency key = `stock:timeframe:candle_date:scanner`).

**Late-in-day recovery (e.g., server back at 22:00):**
- Check if today's **confirmed daily candle** is already stored. If not → fetch it, update derived weekly/monthly, recompute all parameters, run all three scanner engines. Signal generation stays correct regardless of when the server came back.

**Idempotency & dedupe:** every signal write is upserted on `(instrument, source, scanner, timeframe, candle_date)` so re-runs never double-send.

---

## 10. Notifications (Telegram)

- Per-user configurable bot (`bot_token` + `chat_id`), with filters (timeframes, min confidence, watchlist-only).
- **Outbound message** format includes: stock, **which scanner(s)** fired, **timeframe** (1D/1W/1M), direction, and for weekly the **`N/4` confidence** + names of the firing scanners. BUY signals include an inline **"Paper Buy"** button.
- **Inbound commands / callbacks:** `/papertrade <symbol>`, buttons to open a paper trade, `/portfolio`, `/pnl`, `/mute`. Routed into the paper engine with the same market-aware logic as the web app.
- Delivery is queued via Redis with retry so a Telegram outage doesn't lose alerts.

---

## 11. Analytics dashboard + Excel export

- **Dashboard API** aggregates: computed technical parameters per stock/timeframe, historical performance, scanner insights (hit counts, confidence distribution), and market intelligence.
- **Filter / sort / customize** server-side (timeframe, scanner, confidence, watchlist, date range) with Redis-cached hot queries.
- **Excel export** via `excelize`: exports the *current filtered* dashboard view — one sheet for analytics params, one for signal history, one for paper-trade performance — with formatting, headers, and autofilter. Endpoint streams `.xlsx` for offline analysis/reporting.

---

## 12. Paper trading

- User configures **virtual capital**; engine tracks cash, positions, equity, realized/unrealized P&L.
- Trades allowed **only on stocks that generated a valid scanner signal** (linked via `signal_id`); BUY signals are entry-eligible.
- **Market-aware execution:** if market **open** → execute immediately at current price (from live WS / last quote); if **closed** → create `SCHEDULED` trade filled at next session open (handled by the paper scheduler).
- Full **trade history**, **portfolio performance**, **P&L** tracking; initiate from **web or Telegram**; exits via signal reversal, manual close, or rules (extensible).

---

## 13. Redis usage

Latest candles & indicators cache · active/recent signals cache · scanner result cache · **rate-limiter token bucket** for Angel API · **idempotency keys** for signal dedupe · Telegram delivery queue · live LTP cache · session/token store · pub/sub to push UI updates.

---

## 14. Core REST API (indicative)

```
POST /auth/login  /auth/refresh
GET/POST/DELETE   /watchlists  /watchlists/{id}/items
GET   /stocks/{token}/candles?tf=1D|1W|1M
GET   /stocks/{token}/indicators?tf=...
GET   /signals?tf=&scanner=&minConfidence=&from=&to=      (audit browse)
GET   /analytics?filters...           GET /analytics/export.xlsx
POST  /paper/accounts  POST /paper/trades  GET /paper/portfolio  GET /paper/pnl
POST  /telegram/config                 POST /admin/sync/reconcile
WS    /ws/live                         (LTP + signal push to React)
```

---

## 15. Frontend (React)

Responsive SPA: Watchlist manager · Live prices (WS) · Signal feed (filter by scanner/timeframe/confidence) · Analytics dashboard (sortable/filterable tables + charts) with **Export to Excel** · Signal audit browser · Paper trading (config capital, open/close trades, portfolio + P&L) · Telegram settings. State via React Query; charts via a lightweight charting lib.

---

## 16. Phased delivery roadmap

**Phase 0 — Foundations:** repo, config, Postgres+Redis, migrations, Angel auth + scrip master, trading calendar + holiday seed.

**Phase 1 — Data backbone:** instrument search/autocomplete over scrip master, watchlist add → **dynamic history bootstrap** (compute required lookback, one-shot ≤2000 fetch), daily candle store, weekly/monthly aggregation, `sync_state`, basic backfill.

**Phase 2 — Indicators + Pine scanner:** indicator engine (full replay + incremental), Pine port on D/W/M with state machine, `signals` audit, scanner_runs.

**Phase 3 — Weekly scanners + confidence:** 4 Chartink scanners on weekly, `N/4` confidence, reason_json.

**Phase 4 — Scheduler + resilience:** daily-close job, weekly/monthly confirmation, startup + late-recovery reconciliation, idempotent re-runs, retention cleanup.

**Phase 5 — Fan-out + Notifications:** signal distributor (watchlist ∩ scanner-pref join → `signal_deliveries`), user scanner prefs, Telegram outbound (scanner + timeframe + confidence), inbound commands, delivery queue.

**Phase 6 — Analytics + Excel export:** dashboard aggregation API, filters/sort, `.xlsx` export.

**Phase 7 — Paper trading:** virtual capital, market-aware execution, scheduled fills, portfolio/P&L, web + Telegram entry.

**Phase 8 — Frontend + live WS:** React app, live prices, dashboards, paper trading UI.

**Phase 9 — Hardening:** load test concurrent scans + fan-out, rate-limit tuning, observability (metrics/logs/alerts), **local Docker Compose** (Postgres + Redis + Go + React) for the current single-machine run.

---

## 17. Key risks & mitigations

| Risk | Mitigation |
|---|---|
| Angel historical **rate limits** on large universes | Redis token bucket, batching, off-peak bulk sync, cache aggressively, derive higher TFs locally (no extra calls). |
| Pine **stateful** logic drift after recovery | Deterministic **full replay** from candle 0; persist state columns; idempotent dedupe. |
| Weekly EMA(200) needs deep history | 2000 daily candles ≈ 8 yrs covers it; validate min-history before scanning, skip stocks with insufficient data. |
| **Partial candle** signals (weekly/monthly fire early by design) | Built only from *confirmed daily* bars (never intraday); daily TF still requires a closed candle; per-period dedupe prevents repeat fires as the forming candle is re-scanned daily. |
| Duplicate alerts on re-run | Idempotency key `stock:tf:date:scanner`. |
| Holiday vs outage confusion | Maintained `market_holidays` + calendar classification before backfill. |
| Timezone bugs | Everything pinned to `Asia/Kolkata`. |

---

## 18. Resolved decisions

1. **Universe** = union of all users' watchlisted stocks (not full NSE). Stocks added only from the Angel scrip-master autocomplete.
2. **Retention** = **30 days**, daily cleanup from Postgres + Redis.
3. **Weekly threshold** = fires at **N ≥ 1** of 4 (confidence shown as `N/4`).
4. **Multi-user**, with per-user watchlists, scanner prefs, Telegram config, and paper accounts. Scanning is compute-once/fan-out (Section 7.4).
5. **Deployment** = run **locally** for now via Docker Compose (Postgres + Redis + Go server + React). Cloud/K8s deferred.

### Still worth confirming
- **Warmup buffer** size for EMA(200) weekly accuracy (seed-from-SMA vs fetch extra bars) — affects exactness of early weekly signals.
- Whether the **daily Pine scanner** should also be watchlist-gated the same way (assumed yes — same fan-out for all timeframes).
- Local Angel-One credentials handling (single shared API session for platform-wide historical fetch, since data is not user-specific).
