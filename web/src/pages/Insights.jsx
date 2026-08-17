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

// Renders the day's FII/DII snapshot onto an off-screen canvas and downloads
// it as a PNG — a branded card easy to share on WhatsApp/Telegram, no
// screenshot cropping required.
async function downloadFiiDiiImage(snap) {
  await document.fonts.ready; // avoid drawing with fallback fonts before the real ones load

  const scale = 2, W = 720, H = 400;
  const canvas = document.createElement("canvas");
  canvas.width = W * scale;
  canvas.height = H * scale;
  const ctx = canvas.getContext("2d");
  ctx.scale(scale, scale);

  ctx.fillStyle = "#0d0f13";
  ctx.fillRect(0, 0, W, H);
  ctx.strokeStyle = "#1d2129";
  ctx.strokeRect(0.5, 0.5, W - 1, H - 1);

  ctx.fillStyle = "#8b8bff";
  ctx.font = "700 18px 'Space Grotesk', sans-serif";
  ctx.fillText(">_ TradeNexus", 32, 46);

  ctx.fillStyle = "#e8eaed";
  ctx.font = "700 26px 'Space Grotesk', sans-serif";
  ctx.fillText("FII / DII Activity", 32, 88);

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 14px 'JetBrains Mono', monospace";
  ctx.fillText(`NSE cash market, in ₹ crore — as of ${snap.date}`, 32, 112);

  const colX = [32, 220, 400, 560];
  const headY = 160;
  ctx.font = "600 13px 'JetBrains Mono', monospace";
  ["", "Buy", "Sell", "Net"].forEach((h, i) => ctx.fillText(h, colX[i], headY));

  ctx.strokeStyle = "#1d2129";
  ctx.beginPath();
  ctx.moveTo(32, headY + 14);
  ctx.lineTo(W - 32, headY + 14);
  ctx.stroke();

  [["DII", snap.dii], ["FII", snap.fii]].forEach(([label, f], i) => {
    const y = headY + 56 + i * 54;
    ctx.fillStyle = "#e8eaed";
    ctx.font = "700 17px 'Space Grotesk', sans-serif";
    ctx.fillText(label, colX[0], y);
    ctx.font = "500 16px 'JetBrains Mono', monospace";
    ctx.fillText(`₹${fmt(f.buy_value)} Cr`, colX[1], y);
    ctx.fillText(`₹${fmt(f.sell_value)} Cr`, colX[2], y);
    ctx.fillStyle = f.net_value >= 0 ? "#3ecf8e" : "#f0616d";
    ctx.fillText(`${f.net_value >= 0 ? "+" : ""}₹${fmt(f.net_value)} Cr`, colX[3], y);
  });

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 12px 'JetBrains Mono', monospace";
  ctx.fillText(`Generated ${new Date().toLocaleString("en-IN")}`, 32, H - 22);

  canvas.toBlob((blob) => {
    if (!blob) return;
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `fii-dii-${snap.date}.png`;
    a.click();
    URL.revokeObjectURL(url);
  }, "image/png");
}

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
        <div className="row" style={{ gap: 8 }}>
          <button className="btn-sm" onClick={() => downloadFiiDiiImage(snap)} title="Download this snapshot as a shareable image">
            Download image
          </button>
          {isAdmin && (
            <button className="btn-sm" onClick={sendAlert} disabled={busy} title="Send this snapshot to the Telegram stock-signal topic now">
              {busy ? "Sending…" : "Send alert now"}
            </button>
          )}
        </div>
      </div>
      <table>
        <thead>
          <tr><th></th><th>Buy</th><th>Sell</th><th>Net</th></tr>
        </thead>
        <tbody>
          {rows.map(([label, f]) => (
            <tr key={label}>
              <td data-label="">{label}</td>
              <td data-label="Buy">₹{fmt(f.buy_value)} Cr</td>
              <td data-label="Sell">₹{fmt(f.sell_value)} Cr</td>
              <td data-label="Net" className={f.net_value >= 0 ? "text-green" : "text-red"}>
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

/* ---- FII/DII weekly/monthly trend ---- */
function FiiDiiTrend() {
  const [period, setPeriod] = useState("weekly");
  const [points, setPoints] = useState(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    setPoints(null);
    setErr("");
    api.get(`/v1/insights/fii-dii/history?period=${period}&count=${period === "monthly" ? 12 : 16}`)
      .then((r) => setPoints(r.points || []))
      .catch((e) => setErr(e.message));
  }, [period]);

  const fmtPeriod = (d) => {
    const t = new Date(String(d).slice(0, 10) + "T00:00:00");
    if (isNaN(t)) return d;
    return period === "monthly"
      ? t.toLocaleDateString("en-IN", { month: "short", year: "2-digit" })
      : t.toLocaleDateString("en-IN", { day: "2-digit", month: "short" });
  };

  return (
    <div className="panel" style={{ padding: 16, marginBottom: 20 }}>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
        <div>
          <div className="section-title" style={{ margin: "0 0 4px" }}>FII / DII Net Trend</div>
          <div className="subtle" style={{ marginBottom: 12 }}>
            Net buy (+) / sell (−) per {period === "monthly" ? "month" : "week"}, in ₹ crore — spot whether flows have turned net positive or negative.
          </div>
        </div>
        <div className="row">
          <button className={"chip chip-tab" + (period === "weekly" ? " is-active" : "")} onClick={() => setPeriod("weekly")}>Weekly</button>
          <button className={"chip chip-tab" + (period === "monthly" ? " is-active" : "")} onClick={() => setPeriod("monthly")}>Monthly</button>
        </div>
      </div>

      {err ? (
        <div className="err">{err}</div>
      ) : !points ? (
        <div className="spinner">Loading…</div>
      ) : !points.length ? (
        <div className="empty">No FII/DII history stored yet — it builds up day by day going forward.</div>
      ) : (
        <>
          <FiiDiiTotals points={points} />
          <div className="ins-legend fiidii-legend">
            <span><i className="ins-dot ins-dot-buy" /> Net buy</span>
            <span><i className="ins-dot ins-dot-sell" /> Net sell</span>
            <span><i className="fiidii-swatch fiidii-swatch-fii" /> FII</span>
            <span><i className="fiidii-swatch fiidii-swatch-dii" /> DII</span>
          </div>
          <div style={{ overflowX: "auto" }}>
            <FiiDiiBarChart points={points} labelFor={fmtPeriod} />
          </div>
        </>
      )}
    </div>
  );
}

// FiiDiiTotals summarizes the whole visible window into two headline numbers,
// so "was FII/DII net positive or negative overall" is answerable at a glance
// without reading the bars.
function FiiDiiTotals({ points }) {
  const totalFii = points.reduce((a, p) => a + p.fii.net_value, 0);
  const totalDii = points.reduce((a, p) => a + p.dii.net_value, 0);
  return (
    <div className="fiidii-totals">
      {[["FII", totalFii], ["DII", totalDii]].map(([label, v]) => (
        <div className={"fiidii-total-card" + (v >= 0 ? " is-pos" : " is-neg")} key={label}>
          <div className="fiidii-total-label">{label} · this window</div>
          <div className="fiidii-total-value">{v >= 0 ? "+" : ""}₹{fmt(v)} Cr</div>
        </div>
      ))}
    </div>
  );
}

function FiiDiiBarChart({ points, labelFor }) {
  const W = 780, H = 320, padLeft = 68, padRight = 16, padTop = 24, bottomPad = 30;
  const maxAbs = Math.max(1, ...points.flatMap((p) => [Math.abs(p.fii.net_value), Math.abs(p.dii.net_value)]));
  const top = padTop, bottom = H - padTop - bottomPad;
  const zeroY = top + (bottom - top) / 2;
  const scale = (bottom - top) / 2 / maxAbs;
  const y = (v) => zeroY - v * scale;
  const n = points.length;
  const groupW = (W - padLeft - padRight) / n;
  const barW = Math.max(5, Math.min(22, groupW * 0.34));

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="ins-chart fiidii-chart" preserveAspectRatio="xMidYMid meet">
      {[1, 0.5, 0, -0.5, -1].map((f) => (
        <line key={f} x1={padLeft} x2={W - padRight} y1={y(maxAbs * f)} y2={y(maxAbs * f)}
              className={f === 0 ? "ins-grid fiidii-zero-line" : "ins-grid"} />
      ))}
      {points.map((p, i) => {
        const cx = padLeft + groupW * (i + 0.5);
        const fiiTop = Math.min(y(p.fii.net_value), zeroY);
        const fiiH = Math.max(0, Math.abs(y(p.fii.net_value) - zeroY));
        const diiTop = Math.min(y(p.dii.net_value), zeroY);
        const diiH = Math.max(0, Math.abs(y(p.dii.net_value) - zeroY));
        return (
          <g key={i} className="fiidii-bar-group">
            <rect x={cx - barW - 2} y={fiiTop} width={barW} height={fiiH} rx="2.5"
                  className={"fiidii-bar fiidii-bar-fii" + (p.fii.net_value >= 0 ? " is-pos" : " is-neg")}>
              <title>FII net: {p.fii.net_value >= 0 ? "+" : ""}₹{fmt(p.fii.net_value)} Cr ({labelFor(p.period_start)})</title>
            </rect>
            <rect x={cx + 2} y={diiTop} width={barW} height={diiH} rx="2.5"
                  className={"fiidii-bar fiidii-bar-dii" + (p.dii.net_value >= 0 ? " is-pos" : " is-neg")}>
              <title>DII net: {p.dii.net_value >= 0 ? "+" : ""}₹{fmt(p.dii.net_value)} Cr ({labelFor(p.period_start)})</title>
            </rect>
            {(n <= 10 || i % Math.ceil(n / 8) === 0) && (
              <text x={cx} y={bottom + 18} className="ins-axis" textAnchor="middle">{labelFor(p.period_start)}</text>
            )}
          </g>
        );
      })}
      <text x={padLeft - 10} y={y(maxAbs) + 4} className="ins-axis" textAnchor="end">₹{Math.round(maxAbs)}Cr</text>
      <text x={padLeft - 10} y={zeroY + 4} className="ins-axis" textAnchor="end">0</text>
      <text x={padLeft - 10} y={y(-maxAbs) + 4} className="ins-axis" textAnchor="end">-₹{Math.round(maxAbs)}Cr</text>
    </svg>
  );
}

/* ---- Market Breadth ---- */
function Breadth({ points, fiidii, isAdmin }) {
  if (!points.length) {
    return (
      <>
        <FiiDii snap={fiidii} isAdmin={isAdmin} />
        <FiiDiiTrend />
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
      <FiiDiiTrend />
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
