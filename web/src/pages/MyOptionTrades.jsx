import React, { useCallback, useEffect, useState } from "react";
import { api, connectLivePrices } from "../api.js";
import { SummaryStats, TradeCard, unrealizedPnL } from "./optionsShared.jsx";

// Own sidebar page (was the "My Option Trades" tab) — manually-bought
// option positions, same shape as an algo trade but source != options-algo.
export default function MyOptionTrades({ userId }) {
  const [mySum, setMySum] = useState(null);
  const [trades, setTrades] = useState([]);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/summary`),
      api.get(`/v1/users/${userId}/paper/trades`),
    ])
      .then(([m, t]) => { setMySum(m); setTrades(t.trades || []); })
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

  if (!userId) return <div className="empty">Select a user to view your option trades.</div>;
  if (err) return <div className="err">{err}</div>;

  const myTrades = trades.filter(
    (t) => (t.option_type === "CE" || t.option_type === "PE") && t.source !== "options-algo" && t.status === "OPEN"
  );

  return (
    <div>
      <SummaryStats sum={mySum} openTrades={myTrades} />
      {!myTrades.length ? (
        <div className="empty">No manual option positions yet — buy one from the Option Chain page.</div>
      ) : (
        <div className="promoter-grid">
          {myTrades.map((t) => <TradeCard key={t.id} t={t} />)}
        </div>
      )}
    </div>
  );
}
