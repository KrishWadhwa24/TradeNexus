import React, { useCallback, useEffect, useState } from "react";
import { api, convLevel, convLabel } from "../api.js";
import ChartModal from "../components/ChartModal.jsx";


const PATTERN_LABELS = {
  pattern_cup_handle: "Cup and Handle",
  pattern_downtrend_breakout: "Downtrend Breakout",
  pattern_rectangle: "Rectangle Box",
};

// source: "pine" | "weekly" | "patterns"
export default function Scanner({ source, pattern, userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [qty, setQty] = useState({});
  const [searchQuery, setSearchQuery] = useState("");
  const [activeChart, setActiveChart] = useState(null); // { id, symbol }


  const load = useCallback(() => {
    setLoading(true);
    setErr("");
    api
      .get(`/v1/signals?source=${source}&limit=300`)
      .then((r) => {
        const cutoff = Date.now() - 7 * 24 * 3600 * 1000; // last 7 days
        setRows((r.signals || [])
          .filter((s) => new Date(s.created_at).getTime() >= cutoff)
          .filter((s) => !pattern || s.scanner_name === pattern));
      })
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [source, pattern]);

  useEffect(() => { load(); }, [load]);

  async function runScan() {
    setMsg("Starting scan in background…");
    try {
      const r = await api.post("/v1/admin/scan-all", {});
      if (r.status === "already_running") {
        setMsg("A scan is already running — hang tight.");
      } else {
        setMsg("Scan started. Signals will appear shortly.");
        setTimeout(() => { setMsg(""); load(); }, 8000);
      }
    } catch (e) {
      setMsg("Scan failed: " + e.message);
    }
  }

  async function buy(sig) {
    if (!userId) { setMsg("Select a user first (top right)."); return; }
    const q = parseInt(qty[sig.id] || "1", 10);
    try {
      const t = await api.post(`/v1/users/${userId}/paper/trades`, { signal_id: sig.id, quantity: q });
      setMsg(`Paper ${t.status === "SCHEDULED" ? "trade scheduled for next open" : "bought"}: ${t.symbol} x${t.quantity}`);
    } catch (e) {
      setMsg("Buy failed: " + e.message);
    }
  }

  if (loading) return <div className="spinner">Loading signals…</div>;
  if (err) return <div className="err">{err}</div>;

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          {source === "pine" ? "Pine (Chase Momentum)" : source === "patterns" ? PATTERN_LABELS[pattern] : "Weekly scanners"} — current & last 7 days
        </div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <input
            className="btn-sm"
            type="text"
            placeholder="Search symbol"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
          <button className="btn-sm" onClick={runScan}>Run scan now</button>
          <button className="btn-sm" onClick={load}>Refresh</button>
        </div>
      </div>

      {!rows.length ? (
        <div className="empty">No signals in the last 7 days.</div>
      ) : (
        <div className="panel">
          <table className="scanner-table">
            <thead>
              <tr>
                <th>Symbol</th><th>Signal</th><th>Timeframe</th>
                {(source === "weekly" || source === "patterns") && <th>Conviction</th>}
                <th style={{ textAlign: "right" }}>Price</th>
                <th>Scanner(s)</th><th>Candle date</th><th>Action</th>
              </tr>
            </thead>
            <tbody>
              {rows.filter((s) => (s.symbol || "").toLowerCase().includes(searchQuery.toLowerCase())).map((s) => (
                <tr key={s.id}>
                  <td data-label="">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      {s.symbol}
                      <button className="btn-sm btn-ghost" style={{ padding: '2px 6px', fontSize: '11px' }} onClick={() => setActiveChart({ id: s.instrument_id, symbol: s.symbol, tf: s.timeframe })}>Chart</button>
                    </div>
                  </td>
                  <td data-label="Signal"><span className={s.direction === "BUY" ? "tag tag-buy" : "tag tag-sell"}>{s.direction}</span></td>
                  <td data-label="Timeframe">{s.timeframe}</td>
                  {(source === "weekly" || source === "patterns") && (
                    <td data-label="Conviction">
                      {convLevel(source, s.confidence) ? (
                        <span className={"conv conv-" + convLevel(source, s.confidence)}>
                          {convLabel(source, s.confidence)}
                        </span>
                      ) : "—"}
                    </td>
                  )}
                  <td data-label="Price" className="muted" style={{ textAlign: "right" }}>
                    {s.price != null ? `₹${s.price.toFixed(2)}` : "—"}
                  </td>
                  <td data-label="Scanner(s)" className="muted">{PATTERN_LABELS[s.scanner_name] || s.scanner_name}</td>
                  <td data-label="Candle date" className="muted">{s.candle_date?.slice(0, 10)}</td>
                  <td data-label="Action">
                    {s.direction === "BUY" ? (
                      <div className="row" style={{ justifyContent: "flex-end" }}>
                        <input
                          className="qty btn-sm"
                          type="number" min="1" placeholder="qty"
                          value={qty[s.id] || ""}
                          onChange={(e) => setQty({ ...qty, [s.id]: e.target.value })}
                        />
                        <button className="btn-primary btn-sm" onClick={() => buy(s)}>Buy</button>
                      </div>
                    ) : <span className="muted">—</span>}
                  </td>
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
          tf={activeChart.tf}
          onClose={() => setActiveChart(null)}
        />
      )}
    </div>
  );
}
