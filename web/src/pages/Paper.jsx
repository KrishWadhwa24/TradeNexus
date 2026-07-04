import React, { useCallback, useEffect, useState } from "react";
import { api, fmt } from "../api.js";

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

  const pnlCls = sum.total_pnl >= 0 ? "pos" : "neg";

  return (
    <div>
      <div className="grid cards" style={{ marginBottom: 22 }}>
        <Stat label="Invested (open)" value={fmt(sum.invested)} />
        <Stat label="Market value" value={fmt(sum.market_value)} />
        <Stat label="Unrealized P&L" value={fmt(sum.unrealized_pnl)} cls={sum.unrealized_pnl >= 0 ? "pos" : "neg"} />
        <Stat label="Total P&L" value={fmt(sum.total_pnl)} cls={pnlCls} />
        <Stat label="Cash" value={fmt(sum.cash_balance)} />
        <Stat label="Equity" value={fmt(sum.equity)} />
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
