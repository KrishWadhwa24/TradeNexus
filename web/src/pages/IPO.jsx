import React, { useEffect, useState } from "react";
import { api } from "../api.js";

function gmpLevel(pct) {
  if (pct >= 20) return "high";
  if (pct >= 10) return "med";
  return "low";
}

// estProfit = GMP per share × lot size (one lot applied).
function estProfit(x) {
  const lot = parseInt(x.lot, 10);
  if (!x.gmp || !lot || isNaN(lot)) return "—";
  return "₹" + (x.gmp * lot).toLocaleString("en-IN");
}

const TIER_LABEL = {
  apply: "Apply for IPO",
  your_choice: "Your Choice",
  admin_apply: "Apply (admin)",
};

const IG = "https://www.investorgain.com";

export default function IPO({ isAdmin = false }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(null); // ipo id currently applying

  function load() {
    setLoading(true);
    setErr("");
    api
      .get("/v1/ipos")
      .then((r) => setRows(r.ipos || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }

  useEffect(load, []);

  async function refresh() {
    setMsg("Refreshing feed…");
    try {
      await api.post("/v1/admin/ipos/refresh", {});
      setMsg("Feed refresh started — reloading in 6s…");
      setTimeout(load, 6000);
    } catch (e) {
      setMsg("Refresh failed: " + e.message);
    }
  }

  async function apply(x) {
    setBusy(x.id);
    setMsg("");
    try {
      const r = await api.post(`/v1/admin/ipos/${x.id}/apply`, {});
      setMsg(`Sent "Apply (said by admin)" for ${x.name}.`);
      void r;
      load();
    } catch (e) {
      setMsg(`Failed for ${x.name}: ${e.message}`);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>Open &amp; upcoming IPOs — live GMP</div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          {isAdmin && <button className="btn-sm" onClick={refresh}>Refresh feed</button>}
          <button className="btn-sm" onClick={load}>Reload</button>
        </div>
      </div>

      <div className="subtle" style={{ marginBottom: 12 }}>
        GMP data from InvestorGain. On an IPO's last bidding day: GMP ≥ 20% → “Apply for IPO”,
        10–20% → “Your Choice”. Closed/listed IPOs drop off automatically.
      </div>

      {loading ? (
        <div className="spinner">Loading IPOs…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty">No open or upcoming IPOs right now.</div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>IPO</th><th>Board</th><th>Status</th><th>GMP</th><th>Est./lot</th><th>Sub</th>
                <th>Price</th><th>Lot</th><th>Open</th><th>Close</th><th>Listing</th><th>Rating</th>
                {isAdmin && <th>Admin</th>}
              </tr>
            </thead>
            <tbody>
              {rows.map((x) => (
                <tr key={x.id}>
                  <td>
                    {x.url ? (
                      <a href={IG + x.url} target="_blank" rel="noreferrer"><b>{x.name}</b></a>
                    ) : <b>{x.name}</b>}
                    {x.signal_tier && (
                      <span className={"conv conv-" + gmpLevel(x.gmp_percent)} style={{ marginLeft: 8 }}>
                        {TIER_LABEL[x.signal_tier] || x.signal_tier}
                      </span>
                    )}
                  </td>
                  <td className="muted">{x.board || x.category}</td>
                  <td>
                    <span className={"tag " + (x.status === "open" ? "tag-buy" : "tag")}>{x.status}</span>
                  </td>
                  <td>
                    {x.gmp_percent > 0 || x.gmp > 0 ? (
                      <span className={"conv conv-" + gmpLevel(x.gmp_percent)}>
                        ₹{x.gmp} ({x.gmp_percent}%)
                      </span>
                    ) : <span className="muted">—</span>}
                  </td>
                  <td className="muted">{estProfit(x)}</td>
                  <td className="muted">{x.subscription || "—"}</td>
                  <td className="muted">{x.price ? "₹" + x.price : "—"}</td>
                  <td className="muted">{x.lot || "—"}</td>
                  <td className="muted">{fmtDate(x.open_date)}</td>
                  <td className="muted">{fmtDate(x.close_date)}</td>
                  <td className="muted">{fmtDate(x.listing_date)}</td>
                  <td>{"🔥".repeat(Math.max(0, x.rating || 0)) || "—"}</td>
                  {isAdmin && (
                    <td>
                      <button className="btn-sm btn-primary" disabled={busy === x.id} onClick={() => apply(x)}>
                        {busy === x.id ? "Sending…" : "Send Apply"}
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

function fmtDate(d) {
  if (!d) return "—";
  const s = String(d).slice(0, 10);
  const t = new Date(s + "T00:00:00");
  if (isNaN(t.getTime())) return s;
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short" });
}
