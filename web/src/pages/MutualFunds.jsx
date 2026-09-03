import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { api } from "../api.js";
import { Icon } from "../icons.jsx";
import { SkeletonGrid } from "../Skeleton.jsx";
import ShareButton from "../components/ShareButton.jsx";
import { shareCard } from "../shareCard.js";

function fmtNum(n) {
  if (n === null || n === undefined) return "—";
  return Number(n).toLocaleString("en-IN");
}

// Compact rupee value: ₹1.23 Cr / ₹45.6 L / ₹1,234.
function fmtVal(n) {
  const v = Math.abs(Number(n) || 0);
  if (v >= 1e7) return "₹" + (v / 1e7).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " Cr";
  if (v >= 1e5) return "₹" + (v / 1e5).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " L";
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

// Same as fmtVal but "Rs." instead of "₹" — jsPDF's default font can't
// render the rupee glyph.
function fmtValPdf(n) {
  const v = Math.abs(Number(n) || 0);
  if (v >= 1e7) return "Rs." + (v / 1e7).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " Cr";
  if (v >= 1e5) return "Rs." + (v / 1e5).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " L";
  return "Rs." + Math.round(v).toLocaleString("en-IN");
}

// Same 7-slot categorical palette as --mf-1..--mf-7/--mf-other in
// styles.css, hardcoded as hex — canvas fillStyle can't resolve CSS custom
// properties, so the on-screen and downloaded charts share values, not the
// variables themselves.
const MF_HEX = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#6a3fd1", "#4aa3a3"];
const mfColorHex = (i, symbol) => (symbol === "Other" ? "#4a4f5c" : MF_HEX[i % MF_HEX.length]);

// drawFundCharts paints a donut (share of buy_value) + a bar chart (buy_value
// per stock) side by side into any 2D canvas context, at the given box —
// shared by the PNG card (dark) and the PDF chart image (light), so both get
// the same pie+bar the on-screen modal already shows.
function drawFundCharts(ctx, stocks, { x, y, w, h, holeColor, mutedColor }) {
  const totalBuy = stocks.reduce((a, s) => a + s.buy_value, 0) || 1;

  const pieR = Math.min(h, w * 0.42) / 2;
  const pieCx = x + pieR + 6;
  const pieCy = y + h / 2;
  let angle = -Math.PI / 2;
  stocks.forEach((s, i) => {
    const frac = s.buy_value / totalBuy;
    const end = angle + frac * 2 * Math.PI;
    ctx.beginPath();
    ctx.moveTo(pieCx, pieCy);
    ctx.arc(pieCx, pieCy, pieR, angle, end);
    ctx.closePath();
    ctx.fillStyle = mfColorHex(i, s.symbol);
    ctx.fill();
    angle = end;
  });
  ctx.beginPath();
  ctx.arc(pieCx, pieCy, pieR * 0.55, 0, 2 * Math.PI);
  ctx.fillStyle = holeColor;
  ctx.fill();

  const barX = pieCx + pieR + 34;
  const barBottom = y + h - 55;
  const barTop = y + 6;
  const maxBuy = Math.max(1, ...stocks.map((s) => s.buy_value));
  const n = stocks.length;
  const gap = 8;
  const bw = Math.max(6, (x + w - barX - gap * (n - 1)) / n);
  stocks.forEach((s, i) => {
    const bh = (s.buy_value / maxBuy) * (barBottom - barTop);
    const bx = barX + i * (bw + gap);
    ctx.fillStyle = mfColorHex(i, s.symbol);
    ctx.fillRect(bx, barBottom - bh, bw, bh);
    ctx.save();
    ctx.translate(bx + bw / 2, barBottom + 10);
    ctx.rotate(-Math.PI / 4);
    ctx.textAlign = "right";
    ctx.fillStyle = mutedColor;
    ctx.font = "500 9px 'JetBrains Mono', monospace";
    ctx.fillText(s.symbol, 0, 0);
    ctx.restore();
  });
}

// buildFundCardBlob renders a branded shareable card — the fund's headline
// numbers, a pie+bar breakdown of its top holdings, and the full holdings
// table — matching the same canvas-drawing approach already used for the
// FII/DII snapshot (Insights.jsx). Meant for WhatsApp/Telegram sharing, not
// print, hence PNG rather than PDF.
async function buildFundCardBlob(fundName, detail) {
  await document.fonts.ready;

  const top = mfTopStocks(detail.stocks || []).filter((s) => s.buy_value > 0).slice(0, 8);
  const chartH = top.length ? 250 : 0;
  const scale = 2, W = 720, H = 170 + chartH + top.length * 34 + 60;
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
  ctx.fillText(fundName, 32, 88);

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 14px 'JetBrains Mono', monospace";
  ctx.fillText(`Acquired ${fmtVal(detail.buy_value)}  ·  Sold ${fmtVal(detail.sell_value)}  ·  ${(detail.stocks || []).length} stocks held`, 32, 112);

  let tableTop = 148;
  if (top.length) {
    drawFundCharts(ctx, top, { x: 32, y: 128, w: W - 64, h: chartH - 20, holeColor: "#0d0f13", mutedColor: "#7d8590" });
    tableTop = 128 + chartH;
  }

  const colX = [32, 380, 560];
  ctx.font = "600 13px 'JetBrains Mono', monospace";
  ctx.fillStyle = "#7d8590";
  ["Stock", "Acquired", "% of buys"].forEach((h, i) => ctx.fillText(h, colX[i], tableTop));

  ctx.strokeStyle = "#1d2129";
  ctx.beginPath();
  ctx.moveTo(32, tableTop + 14);
  ctx.lineTo(W - 32, tableTop + 14);
  ctx.stroke();

  const totalBuy = top.reduce((a, s) => a + s.buy_value, 0) || 1;
  top.forEach((s, i) => {
    const y = tableTop + 44 + i * 34;
    ctx.fillStyle = "#e8eaed";
    ctx.font = "700 14px 'Space Grotesk', sans-serif";
    ctx.fillText(s.symbol, colX[0], y);
    ctx.font = "500 13px 'JetBrains Mono', monospace";
    ctx.fillStyle = "#3ecf8e";
    ctx.fillText(fmtVal(s.buy_value), colX[1], y);
    ctx.fillStyle = "#7d8590";
    ctx.fillText(((s.buy_value / totalBuy) * 100).toFixed(1) + "%", colX[2], y);
  });

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 12px 'JetBrains Mono', monospace";
  ctx.fillText(`Generated ${new Date().toLocaleString("en-IN")}`, 32, H - 20);

  return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
}

// shareFundCard shares the card via the OS share sheet where supported,
// falling back to a plain download (see shareCard.js). No caption text is
// attached here — unlike the IPO/stock cards, this and Promoter Buying are
// meant to be shared as just the image, nothing added.
async function shareFundCard(fundName, detail) {
  const blob = await buildFundCardBlob(fundName, detail);
  return shareCard({
    blob,
    filename: `${fundName.replace(/\s+/g, "-").toLowerCase()}.png`,
    caption: "",
    title: fundName,
  });
}

// downloadFundPdf mirrors Audit.jsx's PDF pattern (jsPDF + autoTable) — the
// same pie+bar chart (rendered onto a light-background canvas and embedded
// as an image, since jsPDF has no native charting) above a printable,
// tabular version of the full holdings.
function downloadFundPdf(fundName, detail) {
  const doc = new jsPDF();
  doc.setFontSize(14);
  doc.text(`TradeNexus - ${fundName}`, 14, 15);
  doc.setFontSize(10);
  doc.text(
    `Acquired ${fmtValPdf(detail.buy_value)}  |  Sold ${fmtValPdf(detail.sell_value)}  |  generated ${new Date().toLocaleString()}`,
    14, 21
  );

  const top = mfTopStocks(detail.stocks || []).filter((s) => s.buy_value > 0).slice(0, 8);
  let startY = 27;
  if (top.length) {
    const chartCanvas = document.createElement("canvas");
    const cw = 500, ch = 220, cScale = 2;
    chartCanvas.width = cw * cScale;
    chartCanvas.height = ch * cScale;
    const cctx = chartCanvas.getContext("2d");
    cctx.scale(cScale, cScale);
    drawFundCharts(cctx, top, { x: 0, y: 0, w: cw, h: ch, holeColor: "#ffffff", mutedColor: "#444444" });
    doc.addImage(chartCanvas.toDataURL("image/png"), "PNG", 14, startY, 180, (180 * ch) / cw);
    startY += (180 * ch) / cw + 8;
  }

  autoTable(doc, {
    startY,
    head: [["Symbol", "Security", "Net qty", "Net value", "Last deal"]],
    body: (detail.stocks || []).map((s) => [
      s.symbol,
      s.security_name,
      s.net_qty.toLocaleString("en-IN"),
      fmtValPdf(s.net_value),
      fmtDate(s.last_deal_date),
    ]),
    styles: { fontSize: 8 },
    headStyles: { fillColor: [59, 66, 92] },
  });

  doc.save(`${fundName.replace(/\s+/g, "-").toLowerCase()}.pdf`);
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
              <div className="row" style={{ gap: 8, marginBottom: 16 }}>
                <ShareButton className="btn-sm" compact={false} share={() => shareFundCard(fundName, detail)} title="Share this fund's card" />
                <button className="btn-sm" onClick={() => downloadFundPdf(fundName, detail)}>Download PDF</button>
              </div>

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
        Every AMC's permanent position built from NSE bulk/block deals.
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
