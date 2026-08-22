import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api.js";
import { Icon } from "../icons.jsx";
import { SkeletonGrid } from "../Skeleton.jsx";

function fmtNum(n) {
  if (n === null || n === undefined) return "—";
  return Number(n).toLocaleString("en-IN");
}

// Compact rupee value: ₹1.23 Cr / ₹45.6 L / ₹1,234.
function fmtVal(n) {
  const v = Math.abs(Number(n) || 0);
  if (v >= 1e7) return "₹" + (v / 1e7).toFixed(2).replace(/\.?0+$/, "") + " Cr";
  if (v >= 1e5) return "₹" + (v / 1e5).toFixed(2).replace(/\.?0+$/, "") + " L";
  return "₹" + Math.round(v).toLocaleString("en-IN");
}

function fmtDate(d) {
  if (!d) return "—";
  const s = String(d).slice(0, 10);
  const t = new Date(s + "T00:00:00");
  if (isNaN(t.getTime())) return s;
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short", year: "numeric" });
}

function fmtDateTime(d) {
  if (!d) return "—";
  const t = new Date(d);
  if (isNaN(t.getTime())) return d;
  return t.toLocaleString("en-IN", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}

// DetailModal loads and shows one stock's full breakdown (net buyers, net
// sellers, and every raw deal row).
function DetailModal({ type, symbol, onClose }) {
  const [detail, setDetail] = useState(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    api.get(`/v1/${type}-deals/${encodeURIComponent(symbol)}`)
      .then(setDetail)
      .catch((e) => setErr(e.message));
  }, [type, symbol]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = ""; };
  }, []);

  return createPortal(
    <div className="deal-backdrop" role="presentation" onClick={onClose}>
      <div className="deal-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <div className="deal-head">
          <div className="deal-head-main">
            <span className={"deal-tag " + (type === "block" ? "deal-tag-block" : "deal-tag-bulk")}>{type} deal</span>
            <div className="deal-title">{symbol}</div>
            {detail && <div className="deal-subtitle">{detail.security_name}</div>}
          </div>
          <button className="icon-btn" aria-label="Close" onClick={onClose}><Icon.close /></button>
        </div>

        <div className="deal-body">
          {err ? (
            <div className="err">{err}</div>
          ) : !detail ? (
            <div className="spinner">Loading…</div>
          ) : (
            <>
              <div className="deal-hero">
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Bought</span>
                  <span className="deal-hero-val text-green">{fmtVal(detail.buy_value)}</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Sold</span>
                  <span className="deal-hero-val text-red">{fmtVal(detail.sell_value)}</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Shares moved</span>
                  <span className="deal-hero-val">{fmtNum(detail.traded_qty)}</span>
                </div>
              </div>

              <ClientList title="Net Buyers" dot="🟢" rows={detail.net_buyers} tone="text-green" sign="+" />
              <ClientList title="Net Sellers" dot="🔴" rows={detail.net_sellers} tone="text-red" sign="−" />

              <div className="deal-group-title">All deals <span className="deal-count">{detail.rows?.length || 0}</span></div>
              <div className="deal-rows">
                {(detail.rows || []).map((r, i) => (
                  <div className="deal-raw-row" key={i}>
                    <span className="deal-raw-client">{r.client_name}</span>
                    <span className={"chip " + (r.buy_sell === "BUY" ? "chip-buy" : "chip-sell")}>{r.buy_sell}</span>
                    <span className="deal-raw-qty">{fmtNum(r.quantity)}</span>
                    <span className="deal-raw-price">₹{fmtNum(r.price)}</span>
                    <span className="deal-raw-date">{fmtDate(r.date)}</span>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      </div>
    </div>,
    document.body
  );
}

function ClientList({ title, dot, rows, tone, sign }) {
  if (!rows || !rows.length) return null;
  return (
    <>
      <div className="deal-group-title">{dot} {title} <span className="deal-count">{rows.length}</span></div>
      <div className="deal-rows">
        {rows.map((c, i) => {
          const avg = c.net_qty ? Math.abs(c.net_value / c.net_qty) : 0;
          return (
            <div className="deal-client-row" key={i}>
              <span className="deal-client-rank">{i + 1}</span>
              <span className="deal-client-name">{c.client_name}</span>
              <span className="deal-client-nums">
                <b className={tone}>{sign}{fmtNum(Math.abs(c.net_qty))}</b>
                <span className="deal-client-sub">@ ₹{avg.toFixed(2)} · {fmtVal(c.net_value)}</span>
              </span>
            </div>
          );
        })}
      </div>
    </>
  );
}

export default function Deals({ type = "bulk", isAdmin = false }) {
  const [tab, setTab] = useState("deals"); // deals | audit
  const [stocks, setStocks] = useState([]);
  const [audit, setAudit] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(null);
  const [selected, setSelected] = useState(null);
  const [searchQuery, setSearchQuery] = useState("");

  const label = type === "block" ? "Block" : "Bulk";

  function openModal(symbol) {
    setSelected(symbol);
    window.history.pushState({ view: type, modal: "details" }, "");
  }

  function closeModal() {
    window.history.back();
  }

  // Handle browser back button to close modal
  useEffect(() => {
    const onPop = () => {
      if (selected) {
        setSelected(null);
      }
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [selected]);

  function load() {
    setLoading(true);
    setErr("");
    const path = tab === "audit" ? `/v1/${type}-deals/audit` : `/v1/${type}-deals`;
    api.get(path)
      .then((r) => (tab === "audit" ? setAudit(r.alerts || []) : setStocks(r.stocks || [])))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }

  useEffect(load, [type, tab]);

  async function refresh() {
    setMsg("Refreshing feed…");
    try {
      await api.post("/v1/admin/deals/refresh", {});
      setMsg("Feed refresh started — reloading in 6s…");
      setTimeout(() => { setMsg(""); load(); }, 6000);
    } catch (e) {
      setMsg("Refresh failed: " + e.message);
    }
  }

  async function fireAlert(symbol) {
    setBusy(symbol);
    setMsg("");
    try {
      await api.post(`/v1/admin/deals/${type}/${encodeURIComponent(symbol)}/send-alert`, {});
      setMsg(`Alert sent for ${symbol}.`);
    } catch (e) {
      setMsg(`Failed for ${symbol}: ${e.message}`);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          {label} deals — NSE feed{type === "bulk" ? " (net ≥ ₹5 Cr)" : ""}
        </div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <input
            className="btn-sm"
            type="text"
            placeholder="Search stock"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
          {isAdmin && <button className="btn-sm" onClick={refresh}>Refresh feed</button>}
          <button className="btn-sm" onClick={load}>Reload</button>
        </div>
      </div>

      <div className="tabs">
        <div className={"tab" + (tab === "deals" ? " active" : "")} onClick={() => setTab("deals")}>Stocks</div>
        <div className={"tab" + (tab === "audit" ? " active" : "")} onClick={() => setTab("audit")}>Sent Alerts</div>
      </div>

      {loading ? (
        <SkeletonGrid count={6} lines={4} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : tab === "audit" ? (
        !audit.length ? (
          <div className="empty">No {label.toLowerCase()}-deal alerts sent in the last 30 days.</div>
        ) : (
          <div className="panel">
            <table>
              <thead>
                <tr><th>Stock</th><th>Bought</th><th>Sold</th><th>Shares</th><th>Price</th><th>Deal Date</th><th>Alerted</th></tr>
              </thead>
              <tbody>
                {audit.filter((a) => 
                  (a.symbol || "").toLowerCase().includes(searchQuery.toLowerCase()) ||
                  (a.security_name || "").toLowerCase().includes(searchQuery.toLowerCase())
                ).map((a, i) => (
                  <tr key={i}>
                    <td style={{ textAlign: "left" }}>
                      <b>{a.symbol}</b>
                      {a.security_name && <span className="deals-sub">{a.security_name}</span>}
                    </td>
                    <td className="text-green">{fmtVal(a.buy_value)}</td>
                    <td className="text-red">{fmtVal(a.sell_value)}</td>
                    <td>{fmtNum(a.traded_qty)}</td>
                    <td>{a.price ? `₹${a.price.toFixed(2)}` : "—"}</td>
                    <td>{fmtDate(a.deal_date)}</td>
                    <td>{fmtDateTime(a.alerted_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : !stocks.length ? (
        <div className="empty">No {label.toLowerCase()} deals{type === "bulk" ? " above the ₹5 Cr net threshold" : ""} in the last 30 days.</div>
      ) : (
        <div className="promoter-grid">
          {stocks.filter((x) => 
            (x.symbol || "").toLowerCase().includes(searchQuery.toLowerCase()) ||
            (x.security_name || "").toLowerCase().includes(searchQuery.toLowerCase())
          ).map((x) => {
            const buy = x.top_net_qty >= 0;
            return (
              <div className="promoter-card" key={x.symbol}>
                <div className="promoter-card-top">
                  <div>
                    <button className="ipo-name ipo-name-btn deals-symbol" onClick={() => openModal(x.symbol)}>{x.symbol}</button>
                    <span className="promoter-company">{x.security_name}</span>
                  </div>
                  <span className={"chip " + (buy ? "chip-buy" : "chip-sell")}>{buy ? "TOP BUYER" : "TOP SELLER"}</span>
                </div>

                <div className="promoter-meta">
                  <div><span className="k">Bought</span><span className="v text-green">{fmtVal(x.buy_value)}</span></div>
                  <div><span className="k">Sold</span><span className="v text-red">{fmtVal(x.sell_value)}</span></div>
                  <div><span className="k">Shares</span><span className="v">{fmtNum(x.traded_qty)} sh</span></div>
                  <div><span className="k">Buyers / Sellers</span><span className="v">{x.buyer_count} / {x.seller_count}</span></div>
                </div>

                {x.top_net_client && (
                  <div className="promoter-person">
                    <span className="k">Biggest</span>
                    <span className="v">
                      {x.top_net_client} <b className={buy ? "text-green" : "text-red"}>{buy ? "+" : "−"}{fmtNum(Math.abs(x.top_net_qty))}</b>
                    </span>
                  </div>
                )}

                <div className="promoter-foot">
                  <span className="promoter-filed-at">Latest: {fmtDate(x.last_deal_date)}</span>
                  <div className="row" style={{ gap: 8 }}>
                    <button className="btn-sm" onClick={() => openModal(x.symbol)}>Details</button>
                    {isAdmin && (
                      <button className="btn-sm" disabled={busy === x.symbol} onClick={() => fireAlert(x.symbol)}>
                        {busy === x.symbol ? "Sending…" : "Fire alert"}
                      </button>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {selected && <DetailModal type={type} symbol={selected} onClose={closeModal} />}
    </div>
  );
}
