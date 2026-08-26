import React, { useEffect, useState } from "react";
import { api, connectLivePrices, download, fmt, fmtInt, pct } from "../api.js";
import ChartModal from "../components/ChartModal.jsx";


export default function Analytics({ userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [activeChart, setActiveChart] = useState(null); // { id, symbol }
  const [sortKey, setSortKey] = useState(null);
  const [sortDir, setSortDir] = useState("asc"); // "asc" | "desc"

  function toggleSort(key) {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  }

  function sortArrow(key) {
    if (sortKey !== key) return <span className="sort-indicator sort-indicator-idle"> ⇅</span>;
    return <span className="sort-indicator sort-indicator-active">{sortDir === "asc" ? " ▲" : " ▼"}</span>;
  }


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
    return connectLivePrices(userId, {
      onMessage: (event) => {
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
      },
    });
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
          <input
            className="btn-sm"
            type="text"
            placeholder="Search symbol"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
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
                {[
                  ["symbol", "Symbol"], ["price", "Price"], ["pct_change", "Chg%"], ["rsi14", "RSI(14)"],
                  ["ema10", "EMA10"], ["ema20", "EMA20"], ["ema50", "EMA50"], ["sma40", "SMA40"],
                  ["atr14", "ATR(14)"], ["volume", "Volume"], ["vol_sma20", "Vol SMA20"],
                ].map(([key, label]) => (
                  <th key={key} onClick={() => toggleSort(key)} style={{ cursor: "pointer", userSelect: "none" }} title="Click to sort">
                    {label}{sortArrow(key)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows
                .filter((r) => r.symbol.toLowerCase().includes(searchQuery.toLowerCase()))
                .sort((a, b) => {
                  if (!sortKey) return 0;
                  const av = a[sortKey], bv = b[sortKey];
                  if (av == null && bv == null) return 0;
                  if (av == null) return sortDir === "asc" ? -1 : 1;
                  if (bv == null) return sortDir === "asc" ? 1 : -1;
                  const cmp = typeof av === "string" ? av.localeCompare(bv) : av - bv;
                  return sortDir === "asc" ? cmp : -cmp;
                })
                .map((r) => (
                <tr key={r.instrument_id}>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      {r.symbol}
                      <button className="btn-sm btn-ghost" style={{ padding: '2px 6px', fontSize: '11px' }} onClick={() => setActiveChart({ id: r.instrument_id, symbol: r.symbol })}>Chart</button>
                    </div>
                  </td>
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

      {activeChart && (
        <ChartModal
          instrumentId={activeChart.id}
          symbol={activeChart.symbol}
          onClose={() => setActiveChart(null)}
        />
      )}
    </div>
  );
}
