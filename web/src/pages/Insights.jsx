import React, { useEffect, useState } from "react";
import { api } from "../api.js";

function fmtPct(n) {
  const v = Number(n) || 0;
  return (v >= 0 ? "+" : "") + v.toFixed(2) + "%";
}

// hz guards against a missing horizon (e.g. an older API response without d30),
// so a missing field never crashes the render.
const hz = (st) => st || { n: 0, avg_return: 0, win_rate: 0 };

function fmtDate(d) {
  const t = new Date(String(d).slice(0, 10) + "T00:00:00");
  return isNaN(t) ? d : t.toLocaleDateString("en-IN", { day: "2-digit", month: "short" });
}

const TABS = [
  { key: "performance", label: "Signal Performance" },
  { key: "confluence", label: "Confluence Board" },
  { key: "breadth", label: "Market Breadth" },
];

export default function Insights() {
  const [tab, setTab] = useState("performance");
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");

  useEffect(() => {
    setLoading(true);
    setErr("");
    setData(null);
    const path =
      tab === "confluence" ? "/v1/insights/confluence" :
      tab === "breadth" ? "/v1/insights/breadth?days=30" :
      "/v1/insights/performance";
    api.get(path).then(setData).catch((e) => setErr(e.message)).finally(() => setLoading(false));
  }, [tab]);

  return (
    <div>
      <div className="tabs">
        {TABS.map((t) => (
          <div key={t.key} className={"tab" + (tab === t.key ? " active" : "")} onClick={() => setTab(t.key)}>
            {t.label}
          </div>
        ))}
      </div>

      {loading ? (
        <div className="spinner">Loading insights…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : tab === "performance" ? (
        <Performance scanners={data?.scanners || []} />
      ) : tab === "confluence" ? (
        <Confluence stocks={data?.stocks || []} />
      ) : (
        <Breadth points={data?.points || []} />
      )}
    </div>
  );
}

/* ---- Signal Performance ---- */
function Performance({ scanners }) {
  if (!scanners.length) {
    return (
      <div className="empty">
        No matured signal outcomes yet. Performance builds up as signals age past 5/10/20 trading days
        (the recorder snapshots forward returns daily).
      </div>
    );
  }
  return (
    <>
      <p className="subtle" style={{ marginTop: 0 }}>
        Directional forward return after a signal fires (a SELL "wins" when price falls). Higher win-rate and
        avg return = a scanner worth trusting.
      </p>
      <div className="ins-perf-grid">
        {scanners.map((s) => (
          <div className="ins-perf-card" key={s.source + s.timeframe}>
            <div className="ins-perf-title">{s.label}</div>
            <div className="ins-horizons">
              {[["5D", hz(s.d5)], ["10D", hz(s.d10)], ["20D", hz(s.d20)], ["30D", hz(s.d30)]].map(([h, st]) => (
                <div className={"ins-horizon" + (st.n === 0 ? " is-empty" : "")} key={h}>
                  <div className="ins-h-label">{h}</div>
                  {st.n === 0 ? (
                    <>
                      <div className="ins-h-ret ins-h-na">—</div>
                      <div className="ins-winbar" />
                      <div className="ins-h-meta">no data yet</div>
                    </>
                  ) : (
                    <>
                      <div className={"ins-h-ret " + (st.avg_return >= 0 ? "text-green" : "text-red")}>{fmtPct(st.avg_return)}</div>
                      <div className="ins-winbar" title={st.win_rate.toFixed(0) + "% win rate"}>
                        <div
                          className={"ins-winbar-fill " + (st.win_rate >= 50 ? "is-win" : "is-loss")}
                          style={{ width: Math.max(2, Math.min(100, st.win_rate)) + "%" }}
                        />
                      </div>
                      <div className="ins-h-meta">{st.win_rate.toFixed(0)}% win · n={st.n}</div>
                    </>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

/* ---- Confluence Board ---- */
function Confluence({ stocks }) {
  if (!stocks.length) {
    return <div className="empty">No stocks with 2+ aligning bullish signals in the last 7 days.</div>;
  }
  return (
    <>
      <p className="subtle" style={{ marginTop: 0 }}>
        Stocks where multiple independent bullish signals lined up in the last 7 days — scanner BUYs, promoter
        buying, and big bulk/block net buyers. More sources = higher conviction.
      </p>
      <div className="promoter-grid">
        {stocks.map((x) => (
          <div className="promoter-card is-buy" key={x.symbol}>
            <div className="promoter-card-top">
              <div>
                <span className="promoter-symbol">{x.symbol}</span>
                <span className="promoter-company">{x.name}</span>
              </div>
              <span className="ins-score" title="aligning bullish sources">{x.score}/4</span>
            </div>
            <div className="ins-source-chips">
              {x.sources.map((src) => (
                <span className="chip chip-buy" key={src}>{src}</span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

/* ---- Market Breadth ---- */
function Breadth({ points }) {
  if (!points.length) {
    return <div className="empty">No signals in the selected window yet.</div>;
  }
  const W = 760, H = 260, pad = 34;
  const max = Math.max(1, ...points.map((p) => Math.max(p.buys, p.sells)));
  const n = points.length;
  const x = (i) => pad + (n === 1 ? (W - 2 * pad) / 2 : (i / (n - 1)) * (W - 2 * pad));
  const y = (v) => H - pad - (v / max) * (H - 2 * pad);
  const path = (key) => points.map((p, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(p[key]).toFixed(1)}`).join(" ");
  const totalBuys = points.reduce((a, p) => a + p.buys, 0);
  const totalSells = points.reduce((a, p) => a + p.sells, 0);

  return (
    <>
      <p className="subtle" style={{ marginTop: 0 }}>
        Daily BUY vs SELL signal counts across all tracked stocks — a market-mood gauge. More green than red = risk-on.
      </p>
      <div className="ins-legend">
        <span><i className="ins-dot ins-dot-buy" /> Buys ({totalBuys})</span>
        <span><i className="ins-dot ins-dot-sell" /> Sells ({totalSells})</span>
      </div>
      <div className="panel" style={{ padding: 16, overflowX: "auto" }}>
        <svg viewBox={`0 0 ${W} ${H}`} className="ins-chart" preserveAspectRatio="xMidYMid meet">
          {[0, 0.25, 0.5, 0.75, 1].map((f) => (
            <line key={f} x1={pad} x2={W - pad} y1={y(max * f)} y2={y(max * f)} className="ins-grid" />
          ))}
          {[0, 0.5, 1].map((f) => (
            <text key={f} x={pad - 6} y={y(max * f) + 4} className="ins-axis" textAnchor="end">{Math.round(max * f)}</text>
          ))}
          <path d={path("sells")} className="ins-line ins-line-sell" />
          <path d={path("buys")} className="ins-line ins-line-buy" />
          {points.map((p, i) => (
            <g key={i}>
              <circle cx={x(i)} cy={y(p.buys)} r="2.5" className="ins-pt-buy" />
              <circle cx={x(i)} cy={y(p.sells)} r="2.5" className="ins-pt-sell" />
            </g>
          ))}
          {points.map((p, i) =>
            (n <= 12 || i % Math.ceil(n / 8) === 0) ? (
              <text key={"x" + i} x={x(i)} y={H - pad + 16} className="ins-axis" textAnchor="middle">{fmtDate(p.date)}</text>
            ) : null
          )}
        </svg>
      </div>
    </>
  );
}
