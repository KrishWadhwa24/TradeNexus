import React, { useEffect, useState } from "react";
import { api, download, fmt, fmtInt, livePricesURL, pct } from "../api.js";

export default function Analytics({ userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!userId) return;
    setLoading(true);
    setErr("");
    api
      .get(`/v1/users/${userId}/dashboard`)
      .then((r) => setRows(r.rows || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [userId]);

  useEffect(() => {
    if (!userId) return;
    const ws = new WebSocket(livePricesURL(userId));
    ws.onmessage = (event) => {
      try {
        const tick = JSON.parse(event.data);
        if (!tick.instrument_id || !tick.price) return;
        setRows((current) => current.map((row) => {
          if (row.instrument_id !== tick.instrument_id) return row;
          const pctChange = row.prev_close > 0 ? ((tick.price - row.prev_close) / row.prev_close) * 100 : row.pct_change;
          return { ...row, price: tick.price, pct_change: pctChange };
        }));
      } catch {
        // Ignore non-tick control messages.
      }
    };
    return () => ws.close();
  }, [userId]);

  function exportCsv() {
    const cols = ["symbol", "price", "pct_change", "rsi14", "ema10", "ema20", "ema50", "sma40", "atr14", "volume", "vol_sma20"];
    const header = cols.join(",");
    const lines = rows.map((r) => cols.map((c) => r[c]).join(","));
    const csv = [header, ...lines].join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "tradenexus_dashboard.csv";
    a.click();
    URL.revokeObjectURL(url);
  }

  if (!userId) return <div className="empty">Select a user (top right) to view their watchlist dashboard.</div>;
  if (loading) return <div className="spinner">Loading dashboard…</div>;
  if (err) return <div className="err">{err}</div>;

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>Watchlist parameters (live price + indicators)</div>
        <div className="row">
          <button className="btn-sm" onClick={exportCsv} disabled={!rows.length}>Export CSV</button>
          <button className="btn-sm" onClick={() => download("/v1/analytics/export.xlsx", "tradenexus_signals.xlsx").catch((e) => setErr(e.message))}>
            Export signals (.xlsx)
          </button>
        </div>
      </div>

      {!rows.length ? (
        <div className="empty">No watchlist stocks. Add instruments to a watchlist and sync their candles.</div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>Symbol</th><th>Price</th><th>Chg%</th><th>RSI(14)</th>
                <th>EMA10</th><th>EMA20</th><th>EMA50</th><th>SMA40</th>
                <th>ATR(14)</th><th>Volume</th><th>Vol SMA20</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.instrument_id}>
                  <td>{r.symbol}</td>
                  <td>{fmt(r.price)}</td>
                  <td className={r.pct_change >= 0 ? "pos" : "neg"}>{pct(r.pct_change)}</td>
                  <td>{fmt(r.rsi14)}</td>
                  <td>{fmt(r.ema10)}</td>
                  <td>{fmt(r.ema20)}</td>
                  <td>{fmt(r.ema50)}</td>
                  <td>{fmt(r.sma40)}</td>
                  <td>{fmt(r.atr14)}</td>
                  <td>{fmtInt(r.volume)}</td>
                  <td className="muted">{fmtInt(Math.round(r.vol_sma20))}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
