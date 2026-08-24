import React, { useCallback, useEffect, useState } from "react";
import { api, connectLivePrices, fmt } from "../api.js";

function Stat({ label, value, cls }) {
  return (
    <div className="card">
      <div className="label">{label}</div>
      <div className={"value " + (cls || "")}>{value}</div>
    </div>
  );
}

export default function Paper({ userId }) {
  const [sum, setSum] = useState(null);
  const [trades, setTrades] = useState([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/summary`),
      api.get(`/v1/users/${userId}/paper/trades`),
    ])
      .then(([s, t]) => { setSum(s); setTrades(t.trades || []); })
      .catch((e) => setErr(e.message));
  }, [userId]);

  useEffect(() => { load(); }, [load]);

  // Live P&L: patch price/unrealized_pnl into the matching OPEN trade as
  // ticks arrive, same pattern as Home.jsx/Analytics.jsx. The backend's
  // liveInstruments (internal/api/live_handlers.go) subscribes this user's
  // open paper-trade instruments alongside their watchlist ones, so this
  // works even for a symbol that isn't on any watchlist.
  useEffect(() => {
    if (!userId) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (!tick.instrument_id || !tick.price) return;
          setTrades((cur) => cur.map((t) => {
            if (t.status !== "OPEN" || t.instrument_id !== tick.instrument_id) return t;
            return { ...t, current_price: tick.price, unrealized_pnl: (tick.price - t.entry_price) * t.quantity };
          }));
        } catch {
          // Ignore non-tick control messages (heartbeat/ready).
        }
      },
    });
  }, [userId]);

  async function close(id) {
    try {
      await api.post(`/v1/paper/trades/${id}/close`, {});
      setMsg("Position closed.");
      load();
    } catch (e) { setMsg("Close failed: " + e.message); }
  }

  if (!userId) return <div className="empty">Select a user to view paper trading.</div>;
  if (err) return <div className="err">{err}</div>;
  if (!sum) return <div className="spinner">Loading…</div>;

  // market_value/unrealized_pnl/total_pnl/equity move with live ticks, so
  // they're derived from the (live-patched) trades array on every render
  // instead of the one-shot /paper/summary fetch — same formula as
  // Service.Summary (internal/paper/service.go). invested/cash_balance and
  // the closed-trade fields don't move from price ticks, so those still
  // come straight from `sum`.
  const openTrades = trades.filter((t) => t.status === "OPEN");
  const marketValue = openTrades.reduce((s, t) => s + t.current_price * t.quantity, 0);
  const unrealizedPnl = openTrades.reduce((s, t) => s + t.unrealized_pnl, 0);
  const totalPnl = sum.realized_pnl + unrealizedPnl;
  const equity = sum.cash_balance + marketValue;
  const pnlCls = totalPnl >= 0 ? "pos" : "neg";

  return (
    <div>
      <div className="grid cards" style={{ marginBottom: 22 }}>
        <Stat label="Invested (open)" value={fmt(sum.invested)} />
        <Stat label="Market value" value={fmt(marketValue)} />
        <Stat label="Unrealized P&L" value={fmt(unrealizedPnl)} cls={unrealizedPnl >= 0 ? "pos" : "neg"} />
        <Stat label="Total P&L" value={fmt(totalPnl)} cls={pnlCls} />
        <Stat label="Cash" value={fmt(sum.cash_balance)} />
        <Stat label="Equity" value={fmt(equity)} />
      </div>

      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>Positions & trade history</div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <button className="btn-sm" onClick={load}>Refresh</button>
        </div>
      </div>

      {!trades.length ? (
        <div className="empty">No paper trades yet. Buy from the Scanner tab.</div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>Symbol</th><th>Status</th><th>Qty</th><th>Entry</th>
                <th>Current / Exit</th><th>P&L</th><th></th>
              </tr>
            </thead>
            <tbody>
              {trades.map((t) => {
                const live = t.status === "OPEN";
                const pnl = live ? t.unrealized_pnl : t.pnl;
                return (
                  <tr key={t.id}>
                    <td>{t.symbol}</td>
                    <td><span className="tag">{t.status}</span></td>
                    <td>{t.quantity}</td>
                    <td>{t.entry_price ? fmt(t.entry_price) : "—"}</td>
                    <td>{live ? fmt(t.current_price) : t.exit_price ? fmt(t.exit_price) : "—"}</td>
                    <td className={pnl >= 0 ? "pos" : "neg"}>{t.status === "SCHEDULED" ? "—" : fmt(pnl)}</td>
                    <td>{live ? <button className="btn-sm" onClick={() => close(t.id)}>Close</button> : <span className="muted">—</span>}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
