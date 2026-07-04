import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api.js";

// source: "pine" | "weekly"
export default function Scanner({ source, userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [qty, setQty] = useState({});

  const load = useCallback(() => {
    setLoading(true);
    setErr("");
    api
      .get(`/v1/signals?source=${source}&limit=300`)
      .then((r) => {
        const cutoff = Date.now() - 7 * 24 * 3600 * 1000; // last 7 days
        setRows((r.signals || []).filter((s) => new Date(s.created_at).getTime() >= cutoff));
      })
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [source]);

  useEffect(() => { load(); }, [load]);

  async function runScan() {
    setMsg("Running scan on all tracked stocks…");
    try {
      const r = await api.post("/v1/admin/scan-all", {});
      setMsg(`Scan complete (${r.count} stocks). Refreshing…`);
      load();
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
          {source === "pine" ? "Pine (Chase Momentum)" : "Weekly scanners"} — current & last 7 days
        </div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <button className="btn-sm" onClick={runScan}>Run scan now</button>
          <button className="btn-sm" onClick={load}>Refresh</button>
        </div>
      </div>

      {!rows.length ? (
        <div className="empty">No signals in the last 7 days.</div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>Symbol</th><th>Signal</th><th>Timeframe</th>
                {source === "weekly" && <th>Confidence</th>}
                <th>Scanner(s)</th><th>Candle date</th><th>Buy</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td>{s.symbol}</td>
                  <td><span className={s.direction === "BUY" ? "tag tag-buy" : "tag tag-sell"}>{s.direction}</span></td>
                  <td>{s.timeframe}</td>
                  {source === "weekly" && <td>{s.confidence != null ? s.confidence + "/4" : "—"}</td>}
                  <td className="muted">{s.scanner_name}</td>
                  <td className="muted">{s.candle_date?.slice(0, 10)}</td>
                  <td>
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
    </div>
  );
}
