import React, { useEffect, useState } from "react";
import { api, fmt } from "../api.js";
import { SkeletonPerfGrid, SkeletonGrid } from "../Skeleton.jsx";

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

export default function Insights({ isAdmin }) {
  const [tab, setTab] = useState("performance");
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [fiidii, setFiidii] = useState(null);

  useEffect(() => {
    setLoading(true);
    setErr("");
    setData(null);
    const path =
      tab === "confluence" ? "/v1/insights/confluence" :
      tab === "breadth" ? "/v1/insights/breadth?days=30" :
      "/v1/insights/performance";
    api.get(path).then(setData).catch((e) => setErr(e.message)).finally(() => setLoading(false));

    if (tab === "breadth") {
      setFiidii(null);
      api.get("/v1/insights/fii-dii").then(setFiidii).catch(() => setFiidii({ available: false }));
    }
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
        tab === "performance" ? <SkeletonPerfGrid count={4} /> :
        tab === "confluence" ? <SkeletonGrid count={6} lines={2} /> :
        <SkeletonGrid count={3} lines={3} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : tab === "performance" ? (
        <Performance scanners={data?.scanners || []} />
      ) : tab === "confluence" ? (
        <Confluence stocks={data?.stocks || []} />
      ) : (
        <Breadth points={data?.points || []} fiidii={fiidii} isAdmin={isAdmin} />
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

/* ---- FII/DII activity ---- */
const NSE_MONTHS = { Jan: 0, Feb: 1, Mar: 2, Apr: 3, May: 4, Jun: 5, Jul: 6, Aug: 7, Sep: 8, Oct: 9, Nov: 10, Dec: 11 };

function isStaleNseDate(s) {
  const [d, mon, y] = String(s).split("-");
  if (!(mon in NSE_MONTHS)) return false;
  const asOf = new Date(Number(y), NSE_MONTHS[mon], Number(d));
  const today = new Date();
  return !(asOf.getFullYear() === today.getFullYear() && asOf.getMonth() === today.getMonth() && asOf.getDate() === today.getDate());
}

function FiiDii({ snap, isAdmin }) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  async function sendAlert() {
    setBusy(true); setErr(""); setMsg("");
    try {
      await api.post("/v1/admin/fii-dii/send-alert", {});
      setMsg("Sent to Telegram.");
    } catch (e) { setErr(e.message); }
    finally { setBusy(false); }
  }

  if (!snap) {
    return null; // still loading
  }
  if (!snap.available) {
    return <div className="empty" style={{ marginBottom: 20 }}>FII/DII activity hasn't been fetched yet — it's only pulled after market close.</div>;
  }
  const rows = [["DII", snap.dii], ["FII", snap.fii]];
  return (
    <div className="panel fiidii-panel" style={{ padding: 16, marginBottom: 20, overflowX: "auto" }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
        <div>
          <div className="section-title" style={{ margin: "0 0 4px" }}>FII / DII Activity</div>
          <div className="subtle" style={{ marginBottom: 12 }}>
            NSE cash-market buy/sell, in ₹ crore — as of {snap.date}
            {isStaleNseDate(snap.date) ? " (last published trading day)" : ""}
          </div>
        </div>
        {isAdmin && (
          <button className="btn-sm" onClick={sendAlert} disabled={busy} title="Send this snapshot to the Telegram stock-signal topic now">
            {busy ? "Sending…" : "Send alert now"}
          </button>
        )}
      </div>
      <table>
        <thead>
          <tr><th></th><th>Buy</th><th>Sell</th><th>Net</th></tr>
        </thead>
        <tbody>
          {rows.map(([label, f]) => (
            <tr key={label}>
              <td>{label}</td>
              <td>₹{fmt(f.buy_value)} Cr</td>
              <td>₹{fmt(f.sell_value)} Cr</td>
              <td className={f.net_value >= 0 ? "text-green" : "text-red"}>
                {f.net_value >= 0 ? "+" : ""}₹{fmt(f.net_value)} Cr
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {msg && <div className="msg" style={{ marginTop: 12 }}>{msg}</div>}
      {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}
    </div>
  );
}

/* ---- Market Breadth ---- */
function Breadth({ points, fiidii, isAdmin }) {
  if (!points.length) {
    return (
      <>
        <FiiDii snap={fiidii} isAdmin={isAdmin} />
        <div className="empty">No signals in the selected window yet.</div>
      </>
    );
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
      <FiiDii snap={fiidii} isAdmin={isAdmin} />
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
