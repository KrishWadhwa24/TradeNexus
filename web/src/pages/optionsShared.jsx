// Shared building blocks for the options-trading pages (AlgoTrades.jsx,
// MyOptionTrades.jsx, OptionChain.jsx, OptionStatistics.jsx) — split out of
// what used to be one page (Options.jsx) into separate sidebar entries, per
// the same pattern the app already uses elsewhere (each nav item is its own
// page). Not a page itself — nothing here is a default export.
import React, { useCallback, useEffect, useState } from "react";
import { api, connectOptionChainStream, fmt } from "../api.js";

export function Stat({ label, value, cls }) {
  return (
    <div className="card">
      <div className="label">{label}</div>
      <div className={"value " + (cls || "")}>{value}</div>
    </div>
  );
}

export function unrealizedPnL(side, entryPrice, currentPrice, qty) {
  return side === "SELL" ? (entryPrice - currentPrice) * qty : (currentPrice - entryPrice) * qty;
}

// TradeCard is one open option position — algo or manual, same shape either
// way (the only difference is which page it's rendered on).
export function TradeCard({ t }) {
  const pnlCls = (t.unrealized_pnl || 0) >= 0 ? "text-green" : "text-red";
  return (
    <div className="promoter-card">
      <div className="promoter-card-top">
        <span className="promoter-symbol">{t.symbol}</span>
        <span style={{ fontWeight: 700, fontFamily: "var(--font-mono)" }} title="Live price">{fmt(t.current_price)}</span>
      </div>
      <div className="promoter-meta">
        <div><span className="k">Qty (lots)</span><span className="v">{t.quantity}</span></div>
        <div><span className="k">Entry</span><span className="v">{fmt(t.entry_price)}</span></div>
        <div><span className="k">Invested</span><span className="v">{fmt(t.entry_price * t.quantity)}</span></div>
        <div><span className="k">P&amp;L</span><span className={"v " + pnlCls}>{fmt(t.unrealized_pnl)}</span></div>
      </div>
    </div>
  );
}

export function SummaryStats({ sum, openTrades }) {
  const marketValue = openTrades.reduce((s, t) => s + t.entry_price * t.quantity + (t.unrealized_pnl || 0), 0);
  const unrealizedTotal = openTrades.reduce((s, t) => s + (t.unrealized_pnl || 0), 0);
  const totalPnl = (sum?.realized_pnl || 0) + unrealizedTotal;
  const equity = (sum?.cash_balance || 0) + marketValue;
  return (
    <div className="grid cards" style={{ marginBottom: 18 }}>
      <Stat label="Capital" value={fmt(sum?.cash_balance)} />
      <Stat label="Invested" value={fmt(sum?.invested)} />
      <Stat label="Unrealized P&L" value={fmt(unrealizedTotal)} cls={unrealizedTotal >= 0 ? "pos" : "neg"} />
      <Stat label="Total P&L" value={fmt(totalPnl)} cls={totalPnl >= 0 ? "pos" : "neg"} />
      <Stat label="Equity" value={fmt(equity)} />
      <Stat label="Open positions" value={sum?.open_positions ?? 0} />
    </div>
  );
}

export function ChainBrowser({ userId, onTraded }) {
  const [chain, setChain] = useState(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [buying, setBuying] = useState(null); // instrumentId currently submitting

  const load = useCallback(() => {
    setBusy(true); setErr("");
    api.get("/v1/optionsalgo/chain")
      .then(setChain)
      .catch((e) => setErr(e.message))
      .finally(() => setBusy(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  // Live-patch bid/ask/LTP/volume/OI in place as ticks stream in — the chain
  // shape itself (strikes, Greeks, lot size) still only refreshes on load()
  // (manual Refresh, or after a buy). Re-subscribes only when the set of
  // instrument IDs actually changes (e.g. after a Refresh moves the ATM
  // window), not on every unrelated re-render.
  const chainIdsKey = (chain?.chain ?? []).map((q) => q.InstrumentID).join(",");
  useEffect(() => {
    if (!chainIdsKey) return;
    const ids = chainIdsKey.split(",").map(Number);
    const disconnect = connectOptionChainStream(userId, ids, {
      onMessage: (event) => {
        const tick = JSON.parse(event.data);
        if (tick.type || !tick.instrument_id) return; // "ready"/"heartbeat" frames, not a price tick
        setChain((prev) => {
          if (!prev?.chain) return prev;
          return {
            ...prev,
            chain: prev.chain.map((q) =>
              q.InstrumentID === tick.instrument_id
                ? { ...q, LTP: tick.price, Bid: tick.bid ?? 0, Ask: tick.ask ?? 0, Volume: tick.volume ?? 0, OpenInterest: tick.open_interest ?? 0 }
                : q
            ),
          };
        });
      },
    });
    return disconnect;
  }, [chainIdsKey, userId]);

  async function buy(q) {
    if (!q.LotSize) return;
    setBuying(q.InstrumentID);
    try {
      await api.post(`/v1/users/${userId}/paper/trades/open`, {
        instrument_id: q.InstrumentID, quantity: q.LotSize, side: "BUY", product_type: "DELIVERY",
      });
      onTraded?.();
      load();
    } catch (e) {
      setErr("Buy failed: " + e.message);
    } finally {
      setBuying(null);
    }
  }

  return (
    <div className="panel" style={{ padding: 20 }}>
      <div className="toolbar" style={{ marginBottom: 12 }}>
        <div className="section-title" style={{ margin: 0 }}>
          NIFTY option chain {chain && <span className="subtle">— spot {fmt(chain.spot)}, direction {chain.direction}</span>}
        </div>
        <button className="btn-sm" onClick={load} disabled={busy}>{busy ? "Loading…" : "Refresh"}</button>
      </div>
      {err && <div className="err" style={{ marginBottom: 12 }}>{err}</div>}
      {chain && (!chain.chain || chain.chain.length === 0) && (
        <div className="subtle">No chain available right now (outside market hours, or derivatives not yet synced).</div>
      )}
      {chain?.chain?.length > 0 && (
        <div style={{ overflowX: "auto" }}>
          <table>
            <thead>
              <tr>
                <th>Strike</th><th>Type</th><th>LTP</th><th>Bid</th><th>Ask</th>
                <th>Delta</th><th>IV</th><th>OI</th><th>Lot</th><th></th>
              </tr>
            </thead>
            <tbody>
              {[...chain.chain].sort((a, b) => a.StrikePrice - b.StrikePrice || a.OptionType.localeCompare(b.OptionType)).map((q) => (
                <tr key={q.InstrumentID}>
                  <td>{q.StrikePrice}</td>
                  <td><span className={"tag " + (q.OptionType === "CE" ? "tag-buy" : "tag-sell")}>{q.OptionType}</span></td>
                  <td>{fmt(q.LTP)}</td>
                  <td>{fmt(q.Bid)}</td>
                  <td>{fmt(q.Ask)}</td>
                  <td>{q.Delta?.toFixed(3)}</td>
                  <td>{q.IV?.toFixed(2)}</td>
                  <td>{q.OpenInterest}</td>
                  <td>{q.LotSize}</td>
                  <td>
                    <button className="btn-sm btn-primary" disabled={buying === q.InstrumentID} onClick={() => buy(q)}>
                      {buying === q.InstrumentID ? "Buying…" : "Buy 1 lot"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// isoDay formats a Date as YYYY-MM-DD without timezone drift (toISOString
// would shift an IST date back a day for anyone west of UTC).
export function isoDay(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// PnLHeatmap renders one cell per calendar day between from and to, coloured
// by that day's NET P&L (after charges). Days with no closed trades render
// as empty cells, deliberately distinct from a real zero-P&L day.
export function PnLHeatmap({ days, from, to }) {
  const byDate = new Map((days || []).map((d) => [d.date, d]));
  const cells = [];
  const start = new Date(from + "T00:00:00");
  const end = new Date(to + "T00:00:00");
  if (isNaN(start) || isNaN(end) || end < start) return null;

  // Cap the grid so an accidental multi-year range can't render 700+ nodes.
  const MAX_DAYS = 400;
  let magnitude = 0;
  for (const d of byDate.values()) magnitude = Math.max(magnitude, Math.abs(d.net_pnl));

  for (let cur = new Date(start), i = 0; cur <= end && i < MAX_DAYS; cur.setDate(cur.getDate() + 1), i++) {
    const key = isoDay(cur);
    const row = byDate.get(key);
    let bg = "var(--panel-2, #f0f0f0)";
    let title = `${key}: no trades`;
    if (row) {
      // Opacity scales with size relative to the biggest day in range, so
      // the worst/best day is always full-strength and the rest read
      // relative to it.
      const alpha = magnitude > 0 ? 0.25 + 0.75 * (Math.abs(row.net_pnl) / magnitude) : 0.5;
      bg = row.net_pnl >= 0 ? `rgba(34,168,96,${alpha})` : `rgba(220,60,60,${alpha})`;
      title = `${key}: net ${fmt(row.net_pnl)} (gross ${fmt(row.gross_pnl)} − charges ${fmt(row.charges)}) over ${row.trades} trade(s)`;
    }
    cells.push(<div key={key} title={title} style={{ width: 14, height: 14, borderRadius: 3, background: bg }} />);
  }

  return (
    <div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 10 }}>{cells}</div>
      <div className="subtle" style={{ display: "flex", gap: 14, alignItems: "center", fontSize: 12 }}>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span style={{ width: 12, height: 12, borderRadius: 3, background: "rgba(34,168,96,0.9)" }} /> profit
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span style={{ width: 12, height: 12, borderRadius: 3, background: "rgba(220,60,60,0.9)" }} /> loss
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span style={{ width: 12, height: 12, borderRadius: 3, background: "var(--panel-2, #f0f0f0)" }} /> no trades
        </span>
        <span>— all figures are after charges. Hover a day for the breakdown.</span>
      </div>
    </div>
  );
}

// StatisticsSection now fetches its own trade list — it's a standalone page
// (OptionStatistics.jsx) rather than a tab fed by a shared parent fetch.
export function StatisticsSection({ userId }) {
  const today = new Date();
  const ninetyAgo = new Date();
  ninetyAgo.setDate(today.getDate() - 90);

  const [from, setFrom] = useState(isoDay(ninetyAgo));
  const [to, setTo] = useState(isoDay(today));
  const [data, setData] = useState(null);
  const [trades, setTrades] = useState([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setBusy(true); setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/algo-daily-pnl?from=${from}&to=${to}`),
      api.get(`/v1/users/${userId}/paper/trades`),
    ])
      .then(([d, t]) => { setData(d); setTrades(t.trades || []); })
      .catch((e) => setErr(e.message))
      .finally(() => setBusy(false));
  }, [userId, from, to]);

  useEffect(() => { load(); }, [load]);

  const t = data?.totals;
  // Closed algo trades in the selected window — the list under the heatmap.
  const closed = trades.filter(
    (x) => x.source === "options-algo" && x.status === "CLOSED" && x.exit_time &&
           isoDay(new Date(x.exit_time)) >= from && isoDay(new Date(x.exit_time)) <= to
  );

  return (
    <>
      <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
        <div className="section-title" style={{ margin: "0 0 6px" }}>Date range</div>
        <div className="row" style={{ gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          <span className="subtle">to</span>
          <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          <button className="btn-sm btn-primary" onClick={load} disabled={busy}>{busy ? "Loading…" : "Apply"}</button>
        </div>
      </div>

      {err && <div className="err" style={{ marginBottom: 12 }}>{err}</div>}

      {t && (
        <>
          <div className="promoter-grid" style={{ marginBottom: 20 }}>
            <Stat label="TRADES" value={t.trades} />
            <Stat label="GROSS P&L" value={fmt(t.gross_pnl)} cls={t.gross_pnl >= 0 ? "pos" : "neg"} />
            <Stat label="CHARGES PAID" value={fmt(t.charges)} cls="neg" />
            <Stat label="NET P&L (AFTER CHARGES)" value={fmt(t.net_pnl)} cls={t.net_pnl >= 0 ? "pos" : "neg"} />
            <Stat label="EST. INCOME TAX (30%)" value={fmt(t.income_tax_estimate)} cls="neg" />
            <Stat label="NET AFTER INCOME TAX" value={fmt(t.net_after_income_tax)} cls={t.net_after_income_tax >= 0 ? "pos" : "neg"} />
          </div>

          <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
            <div className="section-title" style={{ margin: "0 0 4px" }}>Daily P&amp;L heatmap</div>
            <div className="subtle" style={{ marginBottom: 14 }}>
              One cell per day from {data.from} to {data.to} — green means that day finished in profit after
              all charges, red means it finished in loss.
            </div>
            <PnLHeatmap days={data.days} from={data.from} to={data.to} />
          </div>

          <div className="panel" style={{ padding: 20 }}>
            <div className="section-title" style={{ margin: "0 0 14px" }}>Closed algo trades ({closed.length})</div>
            {!closed.length ? (
              <div className="subtle">No closed algo trades in this range.</div>
            ) : (
              <div style={{ overflowX: "auto" }}>
                <table>
                  <thead>
                    <tr>
                      <th>Exited</th><th>Symbol</th><th>Qty</th><th>Entry</th><th>Exit</th>
                      <th>Gross P&amp;L</th><th>Charges</th><th>Net P&amp;L</th>
                    </tr>
                  </thead>
                  <tbody>
                    {closed.map((x) => {
                      const charges = (x.entry_charges || 0) + (x.exit_charges || 0);
                      const net = (x.pnl || 0) - charges;
                      return (
                        <tr key={x.id}>
                          <td>{new Date(x.exit_time).toLocaleString()}</td>
                          <td>{x.symbol}</td>
                          <td>{x.quantity}</td>
                          <td>{fmt(x.entry_price)}</td>
                          <td>{fmt(x.exit_price)}</td>
                          <td className={x.pnl >= 0 ? "pos" : "neg"}>{fmt(x.pnl)}</td>
                          <td className="neg">{fmt(charges)}</td>
                          <td className={net >= 0 ? "pos" : "neg"}>{fmt(net)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}
    </>
  );
}

export function AlgoToggle({ userId, enabled, onUpdated }) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  async function toggle() {
    setBusy(true); setMsg("");
    try {
      await api.put(`/v1/users/${userId}/paper/algo-enabled`, { enabled: !enabled });
      onUpdated?.();
    } catch (e) { setMsg("Failed: " + e.message); }
    finally { setBusy(false); }
  }

  return (
    <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
      <div className="section-title" style={{ margin: "0 0 6px" }}>Algo trading</div>
      <div className="subtle" style={{ marginBottom: 14 }}>
        When on, the strategy evaluates every minute during market hours and places real (paper)
        trades automatically under your algo capital (set from your Profile page).
      </div>
      <div className="row" style={{ gap: 8, alignItems: "center" }}>
        <span className={"tag " + (enabled ? "tag-buy" : "tag-sell")}>{enabled ? "ON" : "OFF"}</span>
        <button className="btn-sm btn-primary" onClick={toggle} disabled={busy}>
          {busy ? "Saving…" : enabled ? "Turn off" : "Turn on"}
        </button>
        {msg && <span className="msg">{msg}</span>}
      </div>
    </div>
  );
}

export function PerformanceStats({ userId }) {
  const [stats, setStats] = useState(null);
  useEffect(() => {
    if (!userId) return;
    api.get(`/v1/users/${userId}/paper/algo-stats`).then(setStats).catch(() => {});
  }, [userId]);

  if (!stats || stats.TotalTrades === 0) return null;
  const fmtHold = (ns) => {
    const mins = Math.round(ns / 6e10); // nanoseconds -> minutes
    if (mins < 60) return `${mins}m`;
    return `${(mins / 60).toFixed(1)}h`;
  };
  return (
    <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
      <div className="section-title" style={{ margin: "0 0 6px" }}>
        Performance ({stats.TotalTrades} closed trade{stats.TotalTrades === 1 ? "" : "s"})
      </div>
      {!stats.ReadyForTuning && (
        <div className="subtle" style={{ marginBottom: 12 }}>
          Fewer than 30 trades so far — per the strategy's own rule, treat these numbers as provisional
          and don't tune parameters off them yet.
        </div>
      )}
      <div className="grid cards">
        <Stat label="Win rate" value={stats.WinRate.toFixed(1) + "%"} />
        <Stat label="Avg winner" value={fmt(stats.AvgWinner)} cls="pos" />
        <Stat label="Avg loser" value={fmt(stats.AvgLoser)} cls="neg" />
        <Stat label="Profit factor" value={stats.ProfitFactor.toFixed(2)} />
        <Stat label="Expectancy/trade" value={fmt(stats.Expectancy)} cls={stats.Expectancy >= 0 ? "pos" : "neg"} />
        <Stat label="Max drawdown" value={fmt(stats.MaxDrawdown)} />
        <Stat label="Avg holding time" value={fmtHold(stats.AvgHoldingTime)} />
        <Stat label="CE / PE win rate" value={`${stats.CEWinRate.toFixed(0)}% / ${stats.PEWinRate.toFixed(0)}%`} />
      </div>
    </div>
  );
}

export function DecisionLog({ isAdmin }) {
  const [decisions, setDecisions] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    setBusy(true);
    api.get("/v1/admin/optionsalgo/decisions?limit=20")
      .then((d) => setDecisions(d.decisions || []))
      .catch(() => setDecisions([]))
      .finally(() => setBusy(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  if (!isAdmin) return null;
  return (
    <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
      <div className="toolbar" style={{ marginBottom: 12 }}>
        <div className="section-title" style={{ margin: 0 }}>Decision log</div>
        <button className="btn-sm" onClick={load} disabled={busy}>{busy ? "Loading…" : "Refresh"}</button>
      </div>
      <div className="subtle" style={{ marginBottom: 12 }}>
        Every evaluation tick, traded or not — why the algo did or didn't act.
      </div>
      {!decisions?.length ? (
        <div className="empty">No decisions logged yet — the algo only evaluates during live market hours.</div>
      ) : (
        <div style={{ overflowX: "auto" }}>
          <table>
            <thead>
              <tr><th>Time</th><th>Direction</th><th>Action</th><th>Reason</th></tr>
            </thead>
            <tbody>
              {decisions.map((d) => (
                <tr key={d.ID}>
                  <td>{new Date(d.EvaluatedAt).toLocaleString()}</td>
                  <td>{d.Direction}</td>
                  <td><span className="tag">{d.Action}</span></td>
                  <td>{d.Action === "EXIT" ? d.ExitReason : (d.EntryReason || d.SelectionReason || d.DirectionReason)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
