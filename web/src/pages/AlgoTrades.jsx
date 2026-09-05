import React, { useCallback, useEffect, useState } from "react";
import { api, connectLivePrices } from "../api.js";
import { SummaryStats, AlgoToggle, PerformanceStats, DecisionLog, TradeCard, unrealizedPnL } from "./optionsShared.jsx";

// Own sidebar page (was the "Algo Trades" tab on the old combined Options
// page) — auto-traded option positions, the on/off toggle, and performance.
// Algo capital + strategy settings live on the Profile page now, not here.
export default function AlgoTrades({ userId, isAdmin }) {
  const [algoSum, setAlgoSum] = useState(null);
  const [account, setAccount] = useState(null);
  const [trades, setTrades] = useState([]);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/algo-summary`),
      api.get(`/v1/users/${userId}/paper/trades`),
      api.get(`/v1/users/${userId}/paper/account`),
    ])
      .then(([a, t, acct]) => { setAlgoSum(a); setTrades(t.trades || []); setAccount(acct); })
      .catch((e) => setErr(e.message));
  }, [userId]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    if (!userId) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (!tick.instrument_id || !tick.price) return;
          setTrades((cur) => cur.map((t) => {
            if (t.status !== "OPEN" || t.instrument_id !== tick.instrument_id) return t;
            return { ...t, current_price: tick.price, unrealized_pnl: unrealizedPnL(t.side, t.entry_price, tick.price, t.quantity) };
          }));
        } catch { /* ignore heartbeat/ready control frames */ }
      },
    });
  }, [userId]);

  if (!userId) return <div className="empty">Select a user to view algo trades.</div>;
  if (err) return <div className="err">{err}</div>;

  const algoTrades = trades.filter(
    (t) => (t.option_type === "CE" || t.option_type === "PE") && t.source === "options-algo" && t.status === "OPEN"
  );

  return (
    <div>
      <SummaryStats sum={algoSum} openTrades={algoTrades} />
      <AlgoToggle userId={userId} enabled={account?.algo_enabled} onUpdated={load} />
      <PerformanceStats userId={userId} />
      {!algoTrades.length ? (
        <div className="empty">No open algo positions right now — the strategy trades automatically when its conditions are met.</div>
      ) : (
        <div className="promoter-grid">
          {algoTrades.map((t) => <TradeCard key={t.id} t={t} />)}
        </div>
      )}
      <DecisionLog isAdmin={isAdmin} />
    </div>
  );
}
