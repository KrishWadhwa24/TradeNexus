import React, { useEffect, useState } from "react";
import { api, connectLivePrices, fmt, pct } from "../api.js";
import { HeroChart, EmptyArt } from "../icons.jsx";

export default function Home({ userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!userId) return;
    setLoading(true);
    setErr("");
    api.get(`/v1/users/${userId}/dashboard`)
      .then((r) => {
        const ranked = [...(r.rows || [])]
          .sort((a, b) => b.pct_change - a.pct_change)
          .slice(0, 30)
          .map((row) => ({
            instrument_id: row.instrument_id,
            symbol: row.symbol,
            last_close: row.last_close,
            prev_close: row.prev_close,
            pct_change: row.pct_change,
          }));
        setRows(ranked);
      })
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [userId]);

  useEffect(() => {
    if (!userId) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (!tick.instrument_id || !tick.price) return;
          setRows((current) => current.map((row) => {
            if (row.instrument_id !== tick.instrument_id) return row;
            const pctChange = row.prev_close > 0 ? ((tick.price - row.prev_close) / row.prev_close) * 100 : row.pct_change;
            return { ...row, last_close: tick.price, pct_change: pctChange };
          }).sort((a, b) => b.pct_change - a.pct_change));
        } catch {
          // Ignore non-tick control messages.
        }
      },
    });
  }, [userId]);

  return (
    <div>
      <div className="hero">
        <span className="kicker">// TRENDING_NOW</span>
        <h2>Spot the movers<br />before the <span className="accent">crowd.</span></h2>
        <p>Today's top gainers across your watchlist, ranked by daily change. Dig deeper in Analytics and the scanners.</p>
        <HeroChart />
      </div>

      <div className="section-title">Top movers today</div>

      {loading ? (
        <div className="spinner">Loading trending stocks…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty"><EmptyArt /><div>No watchlist data yet. Add stocks to your watchlist and sync them.</div></div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr><th>#</th><th>Symbol</th><th>Last</th><th>Prev close</th><th>Change</th></tr>
            </thead>
            <tbody>
              {rows.map((m, i) => (
                <tr key={m.instrument_id}>
                  <td className="muted">{i + 1}</td>
                  <td><b>{m.symbol}</b></td>
                  <td>{fmt(m.last_close)}</td>
                  <td className="muted">{fmt(m.prev_close)}</td>
                  <td className={m.pct_change >= 0 ? "pos" : "neg"}>{pct(m.pct_change)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
