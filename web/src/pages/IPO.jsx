import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api.js";
import { Icon } from "../icons.jsx";
import { SkeletonGrid } from "../Skeleton.jsx";

function gmpLevel(pct) {
  if (pct >= 20) return "high";
  if (pct >= 10) return "med";
  return "low";
}

// estProfit = GMP per share × lot size (one lot applied).
function estProfit(x) {
  const lot = parseInt(x.lot, 10);
  if (!x.gmp || !lot || isNaN(lot)) return null;
  return "₹" + (x.gmp * lot).toLocaleString("en-IN");
}

function fmtDate(d) {
  if (!d) return "—";
  const s = String(d).slice(0, 10);
  const t = new Date(s + "T00:00:00");
  if (isNaN(t.getTime())) return s;
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short" });
}

const TIER_LABEL = {
  apply: "Apply for IPO",
  your_choice: "Your Choice",
  admin_apply: "Apply (admin)",
};

const IG = "https://www.investorgain.com";

function fmtX(n) {
  return n > 0 ? n.toFixed(2) + "x" : "—";
}

// SubscriptionModal shows the per-category subscription breakdown for one
// IPO — this is what opens when you click the IPO name (instead of jumping
// straight to the external InvestorGain page).
function SubscriptionModal({ ipo, onClose }) {
  const rows = [
    ["QIB", ipo.qib],
    ["SHNI", ipo.shni],
    ["BHNI", ipo.bhni],
    ["NII", ipo.nii],
    ["RII", ipo.rii],
  ];
  // Rendered via a portal straight onto <body> — modals must never inherit
  // an ancestor's layout (a page with tall scrollable content can otherwise
  // throw off a plain `position: fixed` centering, depending on the browser).
  return createPortal(
    <div className="sub-modal-backdrop" role="presentation" onClick={onClose}>
      <div
        className="sub-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="sub-modal-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <div className="sub-modal-badge">Subscription</div>
            <h3 id="sub-modal-title">{ipo.name}</h3>
          </div>
          <button className="icon-btn" aria-label="Close" onClick={onClose}><Icon.close /></button>
        </div>

        <div className="sub-modal-total">
          <span className="sub-modal-total-val">{fmtX(ipo.total_subscription)}</span>
          <span className="sub-modal-total-label">Total subscription</span>
          <span className={"chip " + (ipo.anchor_positive ? "chip-open" : "chip-soft")}>
            {ipo.anchor_positive ? "Anchor booked" : "No anchor"}
          </span>
        </div>

        <div className="sub-modal-grid">
          {rows.map(([label, val]) => (
            <div key={label} className="sub-modal-cell">
              <span className="k">{label}</span>
              <span className="v">{fmtX(val)}</span>
            </div>
          ))}
        </div>

        {ipo.url && (
          <a className="subtle" href={IG + ipo.url} target="_blank" rel="noreferrer">
            View full detail on InvestorGain ↗
          </a>
        )}
      </div>
    </div>,
    document.body
  );
}

export default function IPO({ isAdmin = false }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(null);
  const [selected, setSelected] = useState(null);
  const [searchQuery, setSearchQuery] = useState("");

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
      setTimeout(() => { setMsg(""); load(); }, 6000);
    } catch (e) {
      setMsg("Refresh failed: " + e.message);
    }
  }

  async function apply(x) {
    setBusy(x.id);
    setMsg("");
    try {
      await api.post(`/v1/admin/ipos/${x.id}/apply`, {});
      setMsg(`Sent "Apply (said by admin)" for ${x.name}.`);
      load();
    } catch (e) {
      setMsg(`Failed for ${x.name}: ${e.message}`);
    } finally {
      setBusy(null);
    }
  }

  async function clearSignal(x) {
    setBusy(x.id);
    setMsg("");
    try {
      await api.post(`/v1/admin/ipos/${x.id}/clear-signal`, {});
      setMsg(`Removed the signal badge for ${x.name}.`);
      load();
    } catch (e) {
      setMsg(`Failed to clear ${x.name}: ${e.message}`);
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
          <input
            className="btn-sm"
            type="text"
            placeholder="Search IPO"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
          {isAdmin && <button className="btn-sm" onClick={refresh}>Refresh feed</button>}
          <button className="btn-sm" onClick={load}>Reload</button>
        </div>
      </div>

      {loading ? (
        <SkeletonGrid count={6} lines={5} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty">No open or upcoming IPOs right now.</div>
      ) : (
        <div className="ipo-grid">
          {rows.filter((x) => (x.name || "").toLowerCase().includes(searchQuery.toLowerCase())).map((x) => {
            const profit = estProfit(x);
            const hasGmp = x.gmp > 0 || x.gmp_percent > 0;
            return (
              <div
                className={"ipo-card clickable" + (x.status === "open" ? " is-open" : "")}
                key={x.id}
                onClick={() => setSelected(x)}
              >
                <div className="ipo-card-top">
                  <div className="ipo-id">
                    <span className="ipo-name">{x.name}</span>
                    <div className="ipo-badges">
                      <span className="chip">{x.board || x.category}</span>
                      <span className={"chip " + (x.status === "open" ? "chip-open" : "chip-soon")}>{x.status}</span>
                      {x.rating > 0 && <span className="chip chip-rating">{"🔥".repeat(x.rating)}</span>}
                      {x.qib > 0 && (
                        <span className={"chip " + (x.qib > 5 ? "chip-open" : "chip-soft")} title="QIB subscription">
                          QIB {fmtX(x.qib)}
                        </span>
                      )}
                    </div>
                  </div>
                </div>

                <div className="ipo-gmp-block">
                  <div className="ipo-gmp-left">
                    <span className="ipo-gmp-label">GMP</span>
                    {hasGmp ? (
                      <span className={"ipo-gmp-val conv-text-" + gmpLevel(x.gmp_percent)}>
                        ₹{x.gmp} <small>({x.gmp_percent}%)</small>
                      </span>
                    ) : <span className="ipo-gmp-val muted">—</span>}
                  </div>
                  {profit && (
                    <div className="ipo-profit">
                      <span className="ipo-profit-val">{profit}</span>
                      <span className="ipo-profit-label">profit / lot</span>
                    </div>
                  )}
                </div>

                <div className="ipo-meta">
                  <div><span className="k">Price</span><span className="v">{x.price ? "₹" + x.price : "—"}</span></div>
                  <div><span className="k">Lot</span><span className="v">{x.lot || "—"}</span></div>
                  <div><span className="k">Sub</span><span className="v">{x.subscription && x.subscription !== "-" ? x.subscription : "—"}</span></div>
                  <div><span className="k">Size</span><span className="v">{x.ipo_size || "—"}</span></div>
                </div>

                <div className="ipo-dates">
                  <span><b>Open</b> {fmtDate(x.open_date)}</span>
                  <span><b>Close</b> {fmtDate(x.close_date)}</span>
                  <span><b>Lists</b> {fmtDate(x.listing_date)}</span>
                </div>

                {(x.signal_tier || isAdmin) && (
                  <div className="ipo-foot" onClick={(e) => e.stopPropagation()}>
                    {x.signal_tier && (
                      <span className={"conv conv-" + gmpLevel(x.gmp_percent)}>
                        {TIER_LABEL[x.signal_tier] || x.signal_tier}
                        {isAdmin && (
                          <button
                            className="conv-x"
                            title="Remove this signal for all users (does not touch Telegram)"
                            disabled={busy === x.id}
                            onClick={() => clearSignal(x)}
                          >
                            ×
                          </button>
                        )}
                      </span>
                    )}
                    {isAdmin && (
                      <button className="btn-sm btn-primary" disabled={busy === x.id} onClick={() => apply(x)}>
                        {busy === x.id ? "Sending…" : "Send Apply"}
                      </button>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {selected && <SubscriptionModal ipo={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}
