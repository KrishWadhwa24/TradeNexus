import React, { useEffect, useState } from "react";
import { api } from "../api.js";
import { SkeletonGrid } from "../Skeleton.jsx";

const DAY_OPTIONS = [7, 15, 30, 60];

function fmtNum(n) {
  if (n === null || n === undefined) return "—";
  return Number(n).toLocaleString("en-IN");
}

function fmtINR(n) {
  if (!n) return "—";
  return "₹" + Math.round(n).toLocaleString("en-IN");
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

function who(eventType) {
  return eventType.startsWith("kmp") ? "Director / KMP" : "Promoter";
}

function isBuy(eventType) {
  return eventType.endsWith("_buy");
}

export default function PromoterTrades({ isAdmin = false }) {
  const [days, setDays] = useState(30);
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(null);
  const [scanning, setScanning] = useState(false);
  const [filter, setFilter] = useState("all"); // all | buy | sell
  const [searchQuery, setSearchQuery] = useState("");

  function load(d = days) {
    setLoading(true);
    setErr("");
    api
      .get(`/v1/promoter-trades?days=${d}`)
      .then((r) => setRows(r.trades || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }

  useEffect(() => load(days), [days]);

  async function scanNow() {
    setScanning(true);
    setMsg("Scanning NSE for new filings…");
    try {
      await api.post("/v1/promoter-trades/scan", {});
      setMsg("Scan started — reloading in 8s…");
      setTimeout(() => { setMsg(""); load(); }, 8000);
    } catch (e) {
      setMsg(e.message);
    } finally {
      setScanning(false);
    }
  }

  async function sendAlert(x) {
    setBusy(x.id);
    setMsg("");
    try {
      await api.post(`/v1/admin/promoter-trades/${encodeURIComponent(x.id)}/send-alert`, {});
      setMsg(`Alert sent for ${x.symbol} — ${x.person_name}.`);
      load();
    } catch (e) {
      setMsg(`Failed for ${x.symbol}: ${e.message}`);
    } finally {
      setBusy(null);
    }
  }

  const filteredBySearch = rows.filter((x) => 
    (x.symbol || "").toLowerCase().includes(searchQuery.toLowerCase()) ||
    (x.company_name || "").toLowerCase().includes(searchQuery.toLowerCase())
  );
  const visible = filteredBySearch.filter((x) => filter === "all" || (filter === "buy") === isBuy(x.event_type));
  const buyCount = filteredBySearch.filter((x) => isBuy(x.event_type)).length;
  const sellCount = filteredBySearch.length - buyCount;

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>Promoter &amp; Director/KMP market buys and sells — NSE PIT feed</div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <input
            className="btn-sm"
            type="text"
            placeholder="Search symbol/company"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
          <select className="btn-sm" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {DAY_OPTIONS.map((d) => <option key={d} value={d}>{d} days</option>)}
          </select>
          <button className="btn-sm btn-primary" disabled={scanning} onClick={scanNow}>
            {scanning ? "Scanning…" : "Scan now"}
          </button>
          <button className="btn-sm" onClick={() => load()}>Reload</button>
        </div>
      </div>

      {!loading && !err && rows.length > 0 && (
        <div className="promoter-filters">
          <button className={"chip chip-tab" + (filter === "all" ? " is-active" : "")} onClick={() => setFilter("all")}>
            All <small>{rows.length}</small>
          </button>
          <button className={"chip chip-tab chip-buy" + (filter === "buy" ? " is-active" : "")} onClick={() => setFilter("buy")}>
            Buys <small>{buyCount}</small>
          </button>
          <button className={"chip chip-tab chip-sell" + (filter === "sell" ? " is-active" : "")} onClick={() => setFilter("sell")}>
            Sells <small>{sellCount}</small>
          </button>
        </div>
      )}

      {loading ? (
        <SkeletonGrid count={6} lines={5} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : !visible.length ? (
        <div className="empty">No tracked promoter/director/KMP trades in the last {days} days.</div>
      ) : (
        <div className="promoter-grid">
          {visible.map((x) => {
            const buy = isBuy(x.event_type);
            const qtyDelta = (x.qty_after || 0) - (x.qty_before || 0);
            return (
              <div className="promoter-card" key={x.id}>
                <div className="promoter-card-top">
                  <div>
                    <span className="promoter-symbol">{x.symbol}</span>
                    <span className="promoter-company">{x.company_name}</span>
                  </div>
                  <span className={"chip " + (buy ? "chip-buy" : "chip-sell")}>{buy ? "BUY" : "SELL"}</span>
                </div>

                <div className="promoter-badges">
                  <span className="chip">{who(x.event_type)}</span>
                  <span className="chip chip-soft">{x.category}</span>
                  <span className="chip chip-soft">{x.mode}</span>
                </div>

                <div className="promoter-person">
                  <span className="k">By</span>
                  <span className="v">{x.person_name || "—"}</span>
                </div>

                <div className="promoter-meta">
                  <div><span className="k">Quantity</span><span className="v">{fmtNum(x.quantity)} sh</span></div>
                  <div><span className="k">Value</span><span className="v">{fmtINR(x.value_inr)}</span></div>
                  <div><span className="k">Qty {buy ? "gained" : "reduced"}</span><span className="v">{fmtNum(Math.abs(qtyDelta))}</span></div>
                  <div><span className="k">Trade date</span><span className="v">{fmtDate(x.trade_date_to)}</span></div>
                </div>

                <div className="promoter-holding">
                  <span className="k">Holding</span>
                  <span className="v">
                    {x.pct_before?.toFixed(2)}% <span className="promoter-arrow">→</span>{" "}
                    <b className={buy ? "text-green" : "text-red"}>{x.pct_after?.toFixed(2)}%</b>
                  </span>
                </div>

                <div className="promoter-foot">
                  <div className="row" style={{ gap: 10 }}>
                    {x.filing_url && (
                      <a className="subtle" href={x.filing_url} target="_blank" rel="noreferrer">View filing ↗</a>
                    )}
                    <span className="promoter-filed-at" title="NSE filing timestamp">Filed {fmtDateTime(x.broadcast_at)}</span>
                  </div>
                  {isAdmin && (
                    <div className="row" style={{ gap: 8 }}>
                      {x.alerted && <span className="chip chip-soft">Alerted</span>}
                      <button className="btn-sm" disabled={busy === x.id} onClick={() => sendAlert(x)}>
                        {busy === x.id ? "Sending…" : x.alerted ? "Resend alert" : "Send alert"}
                      </button>
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
