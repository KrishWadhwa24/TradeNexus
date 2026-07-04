import React, { useEffect, useState } from "react";
import { api, fmt, pct } from "../api.js";
import { HeroChart, EmptyArt } from "../icons.jsx";

export default function Home() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");

  useEffect(() => {
    api.get("/v1/market/trending?limit=30")
      .then((r) => setRows(r.trending || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, []);

  return (
    <div>
      <div className="hero">
        <h2>Spot the movers before the crowd.</h2>
        <p>Today's top gainers across your tracked universe, ranked by daily change. Dig deeper in Analytics and the scanners.</p>
        <HeroChart />
      </div>

      <div className="section-title">Top movers today</div>

      {loading ? (
        <div className="spinner">Loading trending stocks…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty"><EmptyArt /><div>No data yet. Add stocks to your watchlist and sync them.</div></div>
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
