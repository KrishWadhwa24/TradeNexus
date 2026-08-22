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

// mfTopStocks caps `stocks` to the top 7 by buy_value + one synthetic
// "Other" row summing the rest, so the pie/bar never need more than the
// fixed --mf-1..--mf-7 + --mf-other palette.
function mfTopStocks(stocks) {
  const sorted = [...stocks].sort((a, b) => b.buy_value - a.buy_value);
  const top = sorted.slice(0, 7);
  const rest = sorted.slice(7);
  if (!rest.length) return top;
  const other = rest.reduce(
    (acc, s) => ({
      symbol: "Other",
      buy_value: acc.buy_value + s.buy_value,
      sell_value: acc.sell_value + s.sell_value,
    }),
    { symbol: "Other", buy_value: 0, sell_value: 0 }
  );
  return [...top, other];
}

const MF_COLORS = ["var(--mf-1)", "var(--mf-2)", "var(--mf-3)", "var(--mf-4)", "var(--mf-5)", "var(--mf-6)", "var(--mf-7)"];
const colorFor = (i, symbol) => (symbol === "Other" ? "var(--mf-other)" : MF_COLORS[i % MF_COLORS.length]);

function FundBarChart({ stocks }) {
  const W = 420, H = 280, padLeft = 16, padRight = 16, padTop = 16, bottomPad = 64;
  const max = Math.max(1, ...stocks.map((s) => s.buy_value));
  const top = padTop, bottom = H - bottomPad;
  const groupW = (W - padLeft - padRight) / Math.max(1, stocks.length);
  const barW = Math.max(10, Math.min(46, groupW * 0.6));

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="ins-chart mf-bar-chart" preserveAspectRatio="xMidYMid meet">
      {stocks.map((s, i) => {
        const h = ((s.buy_value / max) * (bottom - top)) || 0;
        const cx = padLeft + groupW * (i + 0.5);
        return (
          <g key={s.symbol}>
            <rect
              className="mf-bar-rect"
              x={cx - barW / 2}
              y={bottom - h}
              width={barW}
              height={h}
              rx="3"
              fill={colorFor(i, s.symbol)}
            >
              <title>{s.symbol}: {fmtVal(s.buy_value)} acquired</title>
            </rect>
            <text
              x={cx}
              y={bottom + 14}
              className="ins-axis"
              textAnchor="end"
              transform={`rotate(-40 ${cx} ${bottom + 14})`}
            >
              {s.symbol}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

// FundPieChart is a donut built from stacked <circle> stroke-dasharray /
// stroke-dashoffset segments — simpler math than SVG path arcs, and this is
// the first pie chart in the app so it should stay the simplest correct
// approach.
function FundPieChart({ stocks }) {
  const size = 200, r = 72, cx = size / 2, cy = size / 2;
  const circumference = 2 * Math.PI * r;
  const total = Math.max(1, stocks.reduce((a, s) => a + s.buy_value, 0));
  let offset = 0;

  return (
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="mf-pie">
      <circle cx={cx} cy={cy} r={r} fill="none" stroke="var(--border)" strokeWidth="28" />
      {stocks.map((s, i) => {
        const frac = s.buy_value / total;
        const len = frac * circumference;
        const dashoffset = -offset;
        offset += len;
        return (
          <circle
            key={s.symbol}
            cx={cx}
            cy={cy}
            r={r}
            fill="none"
            stroke={colorFor(i, s.symbol)}
            strokeWidth="28"
            strokeDasharray={`${len} ${circumference - len}`}
            strokeDashoffset={dashoffset}
            transform={`rotate(-90 ${cx} ${cy})`}
          >
            <title>{s.symbol}: {(frac * 100).toFixed(1)}% of acquisitions</title>
          </circle>
        );
      })}
    </svg>
  );
}

function FundLegend({ stocks }) {
  const total = Math.max(1, stocks.reduce((a, s) => a + s.buy_value, 0));
  return (
    <div className="mf-legend">
      {stocks.map((s, i) => (
        <div className="mf-legend-row" key={s.symbol}>
          <span className="mf-legend-swatch" style={{ background: colorFor(i, s.symbol) }} />
          <span className="mf-legend-symbol">{s.symbol}</span>
          <span>{fmtVal(s.buy_value)}</span>
          <span className="mf-legend-pct">{((s.buy_value / total) * 100).toFixed(1)}%</span>
        </div>
      ))}
    </div>
  );
}

// FundDetailModal loads and shows one fund's full stock-level breakdown.
function FundDetailModal({ fundName, onClose }) {
  const [detail, setDetail] = useState(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    api.get(`/v1/mutual-funds/${encodeURIComponent(fundName)}`)
      .then(setDetail)
      .catch((e) => setErr(e.message));
  }, [fundName]);

  useEffect(() => {
    const onKey = (e) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = ""; };
  }, []);

  const chartStocks = detail ? mfTopStocks(detail.stocks || []).filter((s) => s.buy_value > 0) : [];

  return createPortal(
    <div className="deal-backdrop" role="presentation" onClick={onClose}>
      <div className="deal-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <div className="deal-head">
          <div className="deal-head-main">
            <span className="deal-tag deal-tag-bulk">mutual fund</span>
            <div className="deal-title">{fundName}</div>
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
                  <span className="deal-hero-label">Acquired</span>
                  <span className="deal-hero-val text-green">{fmtVal(detail.buy_value)}</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Sold</span>
                  <span className="deal-hero-val text-red">{fmtVal(detail.sell_value)}</span>
                </div>
                <div className="deal-hero-div" />
                <div className="deal-hero-stat">
                  <span className="deal-hero-label">Stocks held</span>
                  <span className="deal-hero-val">{(detail.stocks || []).length}</span>
                </div>
              </div>

              {chartStocks.length ? (
                <div className="mf-charts">
                  <div className="mf-pie-wrap"><FundPieChart stocks={chartStocks} /></div>
                  <FundLegend stocks={chartStocks} />
                  <FundBarChart stocks={chartStocks} />
                </div>
              ) : (
                <div className="empty">No acquisitions to chart yet — this fund shows selling activity only.</div>
              )}

              <div className="deal-group-title">
                All holdings <span className="deal-count">{(detail.stocks || []).length}</span>
              </div>
              <div className="deal-rows">
                {(detail.stocks || []).map((s) => (
                  <div className="deal-raw-row" key={s.symbol}>
                    <span className="deal-raw-client">{s.symbol} <span className="promoter-company">{s.security_name}</span></span>
                    <span className={"chip " + (s.net_qty >= 0 ? "chip-buy" : "chip-sell")}>
                      {s.net_qty >= 0 ? "NET BUY" : "NET SELL"}
                    </span>
                    <span className="deal-raw-qty">{fmtNum(s.net_qty)}</span>
                    <span className="deal-raw-price">{fmtVal(s.net_value)}</span>
                    <span className="deal-raw-date">{fmtDate(s.last_deal_date)}</span>
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

export default function MutualFunds() {
  const [funds, setFunds] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [selected, setSelected] = useState(null);

  function openModal(fundName) {
    setSelected(fundName);
    window.history.pushState({ view: "mutual-funds", modal: "details" }, "");
  }
  function closeModal() { window.history.back(); }

  // Handle browser back button to close modal without leaving the page.
  useEffect(() => {
    const onPop = () => { if (selected) setSelected(null); };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, [selected]);

  useEffect(() => {
    setLoading(true);
    setErr("");
    api.get("/v1/mutual-funds")
      .then((r) => setFunds(r.funds || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, []);

  const visible = funds.filter((f) => f.fund_name.toLowerCase().includes(searchQuery.toLowerCase()));

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          Mutual funds — bulk/block deal activity
        </div>
        <div className="row">
          <input
            className="btn-sm"
            type="text"
            placeholder="Search fund"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
        </div>
      </div>
      <p className="subtle" style={{ marginTop: 0 }}>
        Every AMC's permanent position built from NSE bulk/block deals — survives the deals feed's own retention window.
      </p>

      {loading ? (
        <SkeletonGrid count={6} lines={3} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : !visible.length ? (
        <div className="empty">No mutual fund activity tracked yet.</div>
      ) : (
        <div className="mf-grid">
          {visible.map((f) => {
            const gross = f.buy_value + f.sell_value;
            const buyPct = gross > 0 ? (f.buy_value / gross) * 100 : 50;
            return (
              <div className="mf-card" key={f.fund_name}>
                <div className="mf-card-top">
                  <div>
                    <span className="mf-card-eyebrow">Mutual fund</span>
                    <button className="mf-card-name" onClick={() => openModal(f.fund_name)}>
                      {f.fund_name}
                    </button>
                  </div>
                  <span className={"chip " + (f.net_value >= 0 ? "chip-buy" : "chip-sell")}>
                    {f.net_value >= 0 ? "NET BUYER" : "NET SELLER"}
                  </span>
                </div>

                <div className="mf-card-ratio" title={`${buyPct.toFixed(0)}% acquired vs sold`}>
                  <div className="mf-card-ratio-buy" style={{ width: buyPct + "%" }} />
                </div>

                <div className="mf-card-stats">
                  <div><span className="k">Acquired</span><span className="v text-green">{fmtVal(f.buy_value)}</span></div>
                  <div><span className="k">Sold</span><span className="v text-red">{fmtVal(f.sell_value)}</span></div>
                  <div><span className="k">Stocks</span><span className="v">{f.stock_count}</span></div>
                </div>

                <div className="mf-card-foot">
                  <span className="subtle" style={{ fontSize: 12 }}>Last deal: {fmtDate(f.last_deal_date)}</span>
                  <button className="btn-sm" onClick={() => openModal(f.fund_name)}>Details</button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {selected && <FundDetailModal fundName={selected} onClose={closeModal} />}
    </div>
  );
}
