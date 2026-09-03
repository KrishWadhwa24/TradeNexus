import React, { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api.js";
import { Icon } from "../icons.jsx";
import { SkeletonGrid } from "../Skeleton.jsx";

function fmtNum(n) {
  if (n === null || n === undefined) return "—";
  return Number(n).toLocaleString("en-IN");
}

function fmtDate(d) {
  if (!d) return "—";
  const s = String(d).slice(0, 10);
  const t = new Date(s + "T00:00:00");
  if (isNaN(t.getTime())) return s;
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short", year: "numeric" });
}

function initials(name) {
  const parts = name.trim().split(/\s+/);
  return ((parts[0]?.[0] || "") + (parts[parts.length - 1]?.[0] || "")).toUpperCase();
}

// Same 7-slot categorical palette used for mutual fund / promoter charts —
// deterministic per name so a given investor always gets the same color.
const INVESTOR_COLORS = ["var(--mf-1)", "var(--mf-2)", "var(--mf-3)", "var(--mf-4)", "var(--mf-5)", "var(--mf-6)", "var(--mf-7)"];
function colorFor(name) {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return INVESTOR_COLORS[h % INVESTOR_COLORS.length];
}

function rankClass(i) {
  if (i === 0) return "investor-rank is-top1";
  if (i === 1) return "investor-rank is-top2";
  if (i === 2) return "investor-rank is-top3";
  return "investor-rank";
}

// InvestorDetailModal loads and shows one tracked investor's current
// holdings, largest stake first, each with a mini bar sized relative to
// their single biggest position.
function InvestorDetailModal({ investorName, onClose }) {
  const [detail, setDetail] = useState(null);
  const [err, setErr] = useState("");
  const color = colorFor(investorName);

  useEffect(() => {
    api.get(`/v1/big-investors/${encodeURIComponent(investorName)}`)
      .then(setDetail)
      .catch((e) => setErr(e.message));
  }, [investorName]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = ""; };
  }, []);

  const holdings = detail?.holdings || [];
  const maxPct = Math.max(1e-9, ...holdings.map((h) => h.pct_holding));
  const combinedPct = holdings.reduce((a, h) => a + h.pct_holding, 0);

  return createPortal(
    <div className="deal-backdrop" role="presentation" onClick={onClose}>
      <div className="deal-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()} style={{ "--icon-color": color }}>
        <div className="deal-head">
          <div className="deal-head-main">
            <div className="row" style={{ gap: 10 }}>
              <span className="investor-avatar" style={{ "--icon-color": color, width: 34, height: 34, fontSize: 13 }}>
                {initials(investorName)}
              </span>
              <div className="deal-title">{investorName}</div>
            </div>
          </div>
          <button className="icon-btn" aria-label="Close" onClick={onClose}><Icon.close /></button>
        </div>

        <div className="deal-body">
          {err ? (
            <div className="err">{err}</div>
          ) : !detail ? (
            <div className="spinner">Loading…</div>
          ) : !holdings.length ? (
            <div className="empty">No disclosed holdings tracked yet for this investor.</div>
          ) : (
            <>
              <div className="deal-hero">
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Stocks held</span>
                  <span className="deal-hero-val">{holdings.length}</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Combined stake</span>
                  <span className="deal-hero-val" style={{ color }}>{combinedPct.toFixed(2)}%</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Largest position</span>
                  <span className="deal-hero-val">{holdings[0].symbol}</span>
                </div>
              </div>

              <div className="deal-group-title">
                Current holdings <span className="deal-count">{holdings.length}</span>
              </div>
              <div className="deal-rows">
                {holdings.map((h) => (
                  <div className="investor-holding-row" key={h.symbol}>
                    <div className="investor-holding-top">
                      <span className="investor-holding-symbol">
                        {h.symbol} <span className="promoter-company">{h.company_name}</span>
                      </span>
                      <span className="investor-holding-pct" style={{ color }}>{h.pct_holding.toFixed(2)}%</span>
                    </div>
                    <div className="investor-holding-bar">
                      <div className="investor-holding-bar-fill" style={{ width: `${(h.pct_holding / maxPct) * 100}%`, background: color }} />
                    </div>
                    <div className="investor-holding-sub">
                      <span>{fmtNum(h.shares)} shares</span>
                      <span>as of {fmtDate(h.report_date)}</span>
                    </div>
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

export default function BigInvestors() {
  const [investors, setInvestors] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [selected, setSelected] = useState(null);
  const [sortBy, setSortBy] = useState("stocks"); // stocks | conviction

  function openModal(name) {
    setSelected(name);
    window.history.pushState({ view: "big-investors", modal: "details" }, "");
  }
  function closeModal() { window.history.back(); }

  useEffect(() => {
    const onPop = () => { if (selected) setSelected(null); };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [selected]);

  useEffect(() => {
    setLoading(true);
    setErr("");
    api.get("/v1/big-investors")
      .then((r) => setInvestors(r.investors || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, []);

  const visible = useMemo(() => {
    return investors
      .filter((i) => i.investor_name.toLowerCase().includes(searchQuery.toLowerCase()))
      .sort((a, b) => (sortBy === "stocks" ? b.stock_count - a.stock_count : b.top_pct - a.top_pct));
  }, [investors, searchQuery, sortBy]);

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          Big Investor Portfolios
        </div>
        <div className="row">
          <button className={"chip chip-tab" + (sortBy === "stocks" ? " is-active" : "")} onClick={() => setSortBy("stocks")}>
            Most stocks
          </button>
          <button className={"chip chip-tab" + (sortBy === "conviction" ? " is-active" : "")} onClick={() => setSortBy("conviction")}>
            Highest conviction
          </button>
          <input
            className="btn-sm"
            type="text"
            placeholder="Search investor"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
        </div>
      </div>
      <p className="subtle" style={{ marginTop: 0 }}>
        Where India's best-known investors (Vijay Kedia, Radhakishan Damani, Rakesh Jhunjhunwala's RARE Enterprises,
        and others) currently hold a disclosed stake — from NSE's quarterly shareholding-pattern filings.
      </p>

      {loading ? (
        <SkeletonGrid count={6} lines={3} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : !visible.length ? (
        <div className="empty">No tracked holdings yet — filings are picked up as NSE publishes them each quarter.</div>
      ) : (
        <div className="investor-grid">
          {visible.map((inv, i) => {
            const color = colorFor(inv.investor_name);
            return (
              <div className="investor-card" key={inv.investor_name} style={{ "--icon-color": color }}>
                <div className="investor-card-top">
                  <span className="investor-avatar">{initials(inv.investor_name)}</span>
                  <div className="investor-name-block">
                    <div className="investor-name">{inv.investor_name}</div>
                  </div>
                  <span className={rankClass(i)}>#{i + 1}</span>
                </div>

                {inv.top_symbol && (
                  <div className="investor-top-holding">
                    <span className="k">Top holding</span>
                    <span className="v">{inv.top_symbol} · {inv.top_pct.toFixed(2)}%</span>
                  </div>
                )}

                <div className="investor-meta">
                  <div><span className="k">Stocks held</span><span className="v">{inv.stock_count}</span></div>
                  <div><span className="k">Last filing</span><span className="v">{fmtDate(inv.latest_date)}</span></div>
                </div>

                <div className="investor-card-foot">
                  <button className="btn-sm" onClick={() => openModal(inv.investor_name)}>Details</button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {selected && <InvestorDetailModal investorName={selected} onClose={closeModal} />}
    </div>
  );
}
