import React, { useEffect, useState } from "react";
import { api, convLevel, convLabel } from "../api.js";
import { Icon } from "../icons.jsx";

export default function Audit({ isAdmin = false }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [tf, setTf] = useState("");
  const [source, setSource] = useState("");
  const [firing, setFiring] = useState(null); // signal id currently sending
  const [msg, setMsg] = useState("");

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

  async function fire(sig) {
    setFiring(sig.id);
    setMsg("");
    try {
      const r = await api.post(`/v1/admin/dispatch/force?signal_id=${sig.id}`, {});
      const parts = [`sent to ${r.sent} recipient${r.sent === 1 ? "" : "s"}`];
      if (r.default_sent) parts.push("safety-net chat");
      setMsg(`🔥 ${sig.symbol} (${sig.timeframe}) re-fired — ${parts.join(" + ")}.`);
    } catch (e) {
      setMsg(`Failed to re-fire ${sig.symbol}: ${e.message}`);
    } finally {
      setFiring(null);
    }
  }

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          All signals (retained 30 days, then auto-removed)
        </div>
        <div className="row" style={{ flexWrap: "wrap" }}>
          {msg && <span className="msg" style={{ flex: "1 1 100%", marginBottom: 4 }}>{msg}</span>}
          <select value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">All sources</option>
            <option value="pine">Pine</option>
            <option value="weekly">Weekly</option>
            <option value="patterns">Patterns</option>
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

      {isAdmin && (
        <div className="subtle" style={{ marginBottom: 12 }}>
          Admin: use the <b>Fire</b> button to re-send a signal's Telegram alert now — it bypasses the
          duplicate check and the 7-day send window.
        </div>
      )}

      {loading ? (
        <div className="spinner">Loading audit…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty">No signals recorded.</div>
      ) : (
        <div className="panel" style={{ width: "100%" }}>
          <table style={{ minWidth: 720 }}>
            <thead>
              <tr>
                <th>Symbol</th><th>Signal</th><th>Source</th><th>Timeframe</th>
                <th>Conviction</th><th>Scanner(s)</th><th>Candle date</th><th>Generated</th>
                {isAdmin && <th>Alert</th>}
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td>{s.symbol}</td>
                  <td><span className={s.direction === "BUY" ? "tag tag-buy" : "tag tag-sell"}>{s.direction}</span></td>
                  <td className="muted">{s.source}</td>
                  <td>{s.timeframe}</td>
                  <td>
                    {convLevel(s.source, s.confidence) ? (
                      <span className={"conv conv-" + convLevel(s.source, s.confidence)}>
                        {convLabel(s.source, s.confidence)}
                      </span>
                    ) : "—"}
                  </td>
                  <td className="muted">{s.scanner_name}</td>
                  <td className="muted">{s.candle_date?.slice(0, 10)}</td>
                  <td className="muted">{new Date(s.created_at).toLocaleString()}</td>
                  {isAdmin && (
                    <td>
                      <button
                        className="btn-sm fire-btn"
                        title="Re-send this alert on Telegram"
                        disabled={firing === s.id}
                        onClick={() => fire(s)}
                      >
                        <Icon.fire />
                        {firing === s.id ? "Firing…" : "Fire"}
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
