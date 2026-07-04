import React, { useEffect, useState } from "react";
import { api } from "../api.js";

export default function Audit() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [tf, setTf] = useState("");
  const [source, setSource] = useState("");

  function load() {
    setLoading(true);
    setErr("");
    const q = new URLSearchParams({ limit: "500" });
    if (tf) q.set("tf", tf);
    if (source) q.set("source", source);
    api
      .get("/v1/signals?" + q.toString())
      .then((r) => setRows(r.signals || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }

  useEffect(load, [tf, source]);

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          All signals (retained 30 days, then auto-removed)
        </div>
        <div className="row">
          <select value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">All sources</option>
            <option value="pine">Pine</option>
            <option value="weekly">Weekly</option>
          </select>
          <select value={tf} onChange={(e) => setTf(e.target.value)}>
            <option value="">All timeframes</option>
            <option value="1D">1D</option>
            <option value="1W">1W</option>
            <option value="1M">1M</option>
          </select>
          <button className="btn-sm" onClick={load}>Refresh</button>
        </div>
      </div>

      {loading ? (
        <div className="spinner">Loading audit…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty">No signals recorded.</div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>Symbol</th><th>Signal</th><th>Source</th><th>Timeframe</th>
                <th>Confidence</th><th>Scanner(s)</th><th>Candle date</th><th>Generated</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td>{s.symbol}</td>
                  <td><span className={s.direction === "BUY" ? "tag tag-buy" : "tag tag-sell"}>{s.direction}</span></td>
                  <td className="muted">{s.source}</td>
                  <td>{s.timeframe}</td>
                  <td>{s.confidence != null ? s.confidence + "/4" : "—"}</td>
                  <td className="muted">{s.scanner_name}</td>
                  <td className="muted">{s.candle_date?.slice(0, 10)}</td>
                  <td className="muted">{new Date(s.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
