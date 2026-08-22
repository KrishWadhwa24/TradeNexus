import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
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
  if (v >= 1e7) return "₹" + (v / 1e7).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " Cr";
  if (v >= 1e5) return "₹" + (v / 1e5).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " L";
  return "₹" + Math.round(v).toLocaleString("en-IN");
}

function fmtPts(n) {
  const v = Number(n) || 0;
  return (v >= 0 ? "+" : "") + v.toFixed(2) + " pts";
}

function fmtPct(n) {
  const v = Number(n) || 0;
  return (v >= 0 ? "+" : "") + v.toFixed(1) + "%";
}

function fmtDate(d) {
  if (!d) return "—";
  const s = String(d).slice(0, 10);
  const t = new Date(s + "T00:00:00");
  if (isNaN(t.getTime())) return s;
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short", year: "numeric" });
}

// Same as fmtVal but "Rs." instead of "₹" — jsPDF's default font can't
// render the rupee glyph.
function fmtValPdf(n) {
  const v = Math.abs(Number(n) || 0);
  if (v >= 1e7) return "Rs." + (v / 1e7).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " Cr";
  if (v >= 1e5) return "Rs." + (v / 1e5).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " L";
  return "Rs." + Math.round(v).toLocaleString("en-IN");
}

// Same 7-slot categorical palette used for the mutual fund charts, kept
// distinct from the buy/sell semantic green/red used for the point-increase
// bar's direction.
const PT_HEX = ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#6a3fd1", "#4aa3a3"];
const ptColorHex = (i, isOther) => (isOther ? "#4a4f5c" : PT_HEX[i % PT_HEX.length]);

// pbTopByValue caps `people` to the top 7 by buy_value + one synthetic
// "Other" bucket, same capping idea as mfTopStocks — for the pie, which
// answers "who's put in the most money," a different ranking than the
// point-increase bar right next to it.
function pbTopByValue(people) {
  const sorted = [...people].sort((a, b) => b.buy_value - a.buy_value);
  const top = sorted.slice(0, 7);
  const rest = sorted.slice(7);
  if (!rest.length) return top;
  const other = rest.reduce((acc, p) => ({ person_name: "Other", buy_value: acc.buy_value + p.buy_value }), { person_name: "Other", buy_value: 0 });
  return [...top, other];
}

// drawStockCharts paints a donut (share of buy_value, `byValue`) + a
// zero-centered bar chart (point-increase per person, `byIncrease`, green/red
// by direction) side by side — shared by the PNG card (dark) and the PDF
// chart image (light).
function drawStockCharts(ctx, byValue, byIncrease, { x, y, w, h, holeColor, mutedColor }) {
  const totalBuy = byValue.reduce((a, p) => a + p.buy_value, 0) || 1;
  const pieR = Math.min(h, w * 0.42) / 2;
  const pieCx = x + pieR + 6;
  const pieCy = y + h / 2;
  let angle = -Math.PI / 2;
  byValue.forEach((p, i) => {
    const frac = p.buy_value / totalBuy;
    const end = angle + frac * 2 * Math.PI;
    ctx.beginPath();
    ctx.moveTo(pieCx, pieCy);
    ctx.arc(pieCx, pieCy, pieR, angle, end);
    ctx.closePath();
    ctx.fillStyle = ptColorHex(i, p.person_name === "Other");
    ctx.fill();
    angle = end;
  });
  ctx.beginPath();
  ctx.arc(pieCx, pieCy, pieR * 0.55, 0, 2 * Math.PI);
  ctx.fillStyle = holeColor;
  ctx.fill();

  const barX = pieCx + pieR + 34;
  const barTop = y + 6;
  const barBottom = y + h - 55;
  const zeroY = (barTop + barBottom) / 2;
  const maxAbs = Math.max(1, ...byIncrease.map((p) => Math.abs(p.point_increase)));
  const n = byIncrease.length;
  const gap = 8;
  const bw = Math.max(6, (x + w - barX - gap * (n - 1)) / n);
  ctx.save();
  ctx.globalAlpha = 0.4;
  ctx.strokeStyle = mutedColor;
  ctx.beginPath();
  ctx.moveTo(barX, zeroY);
  ctx.lineTo(x + w, zeroY);
  ctx.stroke();
  ctx.restore();
  byIncrease.forEach((p, i) => {
    const bx = barX + i * (bw + gap);
    const bh = (Math.abs(p.point_increase) / maxAbs) * (barBottom - barTop) / 2;
    const buy = p.point_increase >= 0;
    ctx.fillStyle = buy ? "#3ecf8e" : "#f0616d";
    ctx.fillRect(bx, buy ? zeroY - bh : zeroY, bw, bh);
    ctx.save();
    ctx.translate(bx + bw / 2, barBottom + 10);
    ctx.rotate(-Math.PI / 4);
    ctx.textAlign = "right";
    ctx.fillStyle = mutedColor;
    ctx.font = "500 9px 'JetBrains Mono', monospace";
    ctx.fillText(p.person_name.length > 12 ? p.person_name.slice(0, 11) + "…" : p.person_name, 0, 0);
    ctx.restore();
  });
}

// downloadStockImage renders a branded shareable card — the stock's tracked
// promoters ranked by point-increase, a pie+bar breakdown, and the full
// table — same canvas-drawing approach as the FII/DII snapshot
// (Insights.jsx) and the mutual fund cards.
async function downloadStockImage(symbol, detail) {
  await document.fonts.ready;

  const people = (detail.people || []).slice(0, 8);
  const chartH = people.length ? 250 : 0;
  const scale = 2, W = 720, H = 150 + chartH + people.length * 34 + 60;
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
  ctx.fillText(`${symbol} — Promoter Buying`, 32, 88);

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 14px 'JetBrains Mono', monospace";
  ctx.fillText(`${people.length} tracked · permanent position, survives the raw feed's retention window`, 32, 112);

  let tableTop = 148;
  if (people.length) {
    drawStockCharts(ctx, pbTopByValue(people), people, { x: 32, y: 128, w: W - 64, h: chartH - 20, holeColor: "#0d0f13", mutedColor: "#7d8590" });
    tableTop = 128 + chartH;
  }

  const colX = [32, 320, 480, 600];
  ctx.font = "600 13px 'JetBrains Mono', monospace";
  ctx.fillStyle = "#7d8590";
  ["Person", "Stake move", "Increase", "Date"].forEach((h, i) => ctx.fillText(h, colX[i], tableTop));

  ctx.strokeStyle = "#1d2129";
  ctx.beginPath();
  ctx.moveTo(32, tableTop + 14);
  ctx.lineTo(W - 32, tableTop + 14);
  ctx.stroke();

  people.forEach((p, i) => {
    const y = tableTop + 44 + i * 34;
    const buy = p.point_increase >= 0;
    ctx.fillStyle = "#e8eaed";
    ctx.font = "700 13px 'Space Grotesk', sans-serif";
    ctx.fillText(p.person_name.length > 28 ? p.person_name.slice(0, 27) + "…" : p.person_name, colX[0], y);
    ctx.font = "500 12px 'JetBrains Mono', monospace";
    ctx.fillText(`${p.first_pct.toFixed(2)}% → ${p.latest_pct.toFixed(2)}%`, colX[1], y);
    ctx.fillStyle = buy ? "#3ecf8e" : "#f0616d";
    ctx.fillText(`${buy ? "+" : ""}${p.point_increase.toFixed(2)} pts`, colX[2], y);
    ctx.fillStyle = "#7d8590";
    ctx.fillText(new Date(p.latest_date).toLocaleDateString("en-IN", { day: "2-digit", month: "short" }), colX[3], y);
  });

  ctx.fillStyle = "#7d8590";
  ctx.font = "400 12px 'JetBrains Mono', monospace";
  ctx.fillText(`Generated ${new Date().toLocaleString("en-IN")}`, 32, H - 20);

  canvas.toBlob((blob) => {
    if (!blob) return;
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${symbol.toLowerCase()}-promoter-buying.png`;
    a.click();
    URL.revokeObjectURL(url);
  }, "image/png");
}

// downloadStockPdf mirrors Audit.jsx's PDF pattern (jsPDF + autoTable) — the
// same pie+bar chart embedded as an image above the printable table.
function downloadStockPdf(symbol, detail) {
  const doc = new jsPDF();
  doc.setFontSize(14);
  doc.text(`TradeNexus - ${symbol} Promoter Buying`, 14, 15);
  doc.setFontSize(10);
  doc.text(`${(detail.people || []).length} tracked, generated ${new Date().toLocaleString()}`, 14, 21);

  const people = (detail.people || []).slice(0, 8);
  let startY = 27;
  if (people.length) {
    const chartCanvas = document.createElement("canvas");
    const cw = 500, ch = 220, cScale = 2;
    chartCanvas.width = cw * cScale;
    chartCanvas.height = ch * cScale;
    const cctx = chartCanvas.getContext("2d");
    cctx.scale(cScale, cScale);
    drawStockCharts(cctx, pbTopByValue(people), people, { x: 0, y: 0, w: cw, h: ch, holeColor: "#ffffff", mutedColor: "#444444" });
    doc.addImage(chartCanvas.toDataURL("image/png"), "PNG", 14, startY, 180, (180 * ch) / cw);
    startY += (180 * ch) / cw + 8;
  }

  autoTable(doc, {
    startY,
    head: [["Person", "Category", "First %", "Latest %", "Point increase", "Bought", "Sold", "Last disclosure"]],
    body: (detail.people || []).map((p) => [
      p.person_name,
      p.category,
      p.first_pct.toFixed(2) + "%",
      p.latest_pct.toFixed(2) + "%",
      (p.point_increase >= 0 ? "+" : "") + p.point_increase.toFixed(2),
      fmtValPdf(p.buy_value),
      fmtValPdf(p.sell_value),
      fmtDate(p.latest_date),
    ]),
    styles: { fontSize: 8 },
    headStyles: { fillColor: [59, 66, 92] },
  });

  doc.save(`${symbol.toLowerCase()}-promoter-buying.pdf`);
}

// PersonHistoryView shows one person's individual transaction history for
// one stock — rate/qty/date per disclosure. Bound by the raw feed's own
// retention window (PROMOTER_RETENTION_DAYS), unlike the permanent stake
// summary shown one level up.
function PersonHistoryView({ symbol, personName, onBack }) {
  const [trades, setTrades] = useState(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    setTrades(null);
    setErr("");
    api.get(`/v1/promoter-buying/${encodeURIComponent(symbol)}/history?person=${encodeURIComponent(personName)}`)
      .then((r) => setTrades(r.trades || []))
      .catch((e) => setErr(e.message));
  }, [symbol, personName]);

  return (
    <>
      <button className="subtle" onClick={onBack} style={{ marginBottom: 10 }}>← Back to {symbol}</button>
      <div className="deal-group-title">
        {personName} <span className="deal-count">{trades?.length ?? "…"}</span>
      </div>
      {err ? (
        <div className="err">{err}</div>
      ) : !trades ? (
        <div className="spinner">Loading…</div>
      ) : !trades.length ? (
        <div className="empty">No disclosures for this person within the retention window.</div>
      ) : (
        <div className="pb-hist-list">
          {trades.map((t) => {
            const buy = t.event_type.endsWith("_buy");
            const avgPrice = t.quantity ? t.value_inr / t.quantity : 0;
            return (
              <div className="pb-hist-card" key={t.id}>
                <div className="pb-hist-top">
                  <span className={"chip " + (buy ? "chip-buy" : "chip-sell")}>{buy ? "BUY" : "SELL"}</span>
                  <span className="deal-raw-date">{fmtDate(t.trade_date_to)}</span>
                </div>
                <div className="pb-hist-stats">
                  <div><span className="k">Shares</span><span className="v">{fmtNum(t.quantity)}</span></div>
                  <div><span className="k">Price</span><span className="v">₹{avgPrice.toFixed(2)}</span></div>
                  <div><span className="k">Value</span><span className={"v " + (buy ? "text-green" : "text-red")}>{fmtVal(t.value_inr)}</span></div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}

// StockDetailModal loads and shows every tracked person's stake position for
// one stock, largest point-increase first. Clicking a person drills into
// their individual transaction history in the same modal.
function StockDetailModal({ symbol, onClose }) {
  const [detail, setDetail] = useState(null);
  const [err, setErr] = useState("");
  const [historyPerson, setHistoryPerson] = useState(null);

  useEffect(() => {
    api.get(`/v1/promoter-buying/${encodeURIComponent(symbol)}`)
      .then(setDetail)
      .catch((e) => setErr(e.message));
  }, [symbol]);

  useEffect(() => {
    const onKey = (e) => {
      if (e.key !== "Escape") return;
      if (historyPerson) setHistoryPerson(null);
      else onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, historyPerson]);

  useEffect(() => {
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = ""; };
  }, []);

  return createPortal(
    <div className="deal-backdrop" role="presentation" onClick={onClose}>
      <div className="deal-modal" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <div className="deal-head">
          <div className="deal-head-main">
            <span className="deal-tag deal-tag-bulk">promoter buying</span>
            <div className="deal-title">{symbol}</div>
          </div>
          <button className="icon-btn" aria-label="Close" onClick={onClose}><Icon.close /></button>
        </div>

        <div className="deal-body">
          {historyPerson ? (
            <PersonHistoryView symbol={symbol} personName={historyPerson} onBack={() => setHistoryPerson(null)} />
          ) : err ? (
            <div className="err">{err}</div>
          ) : !detail ? (
            <div className="spinner">Loading…</div>
          ) : !detail.people?.length ? (
            <div className="empty">No tracked positions for this stock.</div>
          ) : (
            <>
              <div className="row" style={{ gap: 8, marginBottom: 16 }}>
                <button className="btn-sm" onClick={() => downloadStockImage(symbol, detail)}>Download image</button>
                <button className="btn-sm" onClick={() => downloadStockPdf(symbol, detail)}>Download PDF</button>
              </div>

              <div className="deal-group-title">
                Tracked people <span className="deal-count">{detail.people.length}</span>
              </div>
              <div className="pb-rows">
                {detail.people.map((p) => (
                  <button className="pb-row pb-row-btn" key={p.person_name} onClick={() => setHistoryPerson(p.person_name)}>
                    <span className="pb-row-person">
                      {p.person_name} <span className="promoter-company">{p.category}</span>
                    </span>
                    <span className="pb-row-move">
                      {p.first_pct.toFixed(2)}% <span className="promoter-arrow">→</span>{" "}
                      <b className={p.point_increase >= 0 ? "text-green" : "text-red"}>{p.latest_pct.toFixed(2)}%</b>
                    </span>
                    <span className={"chip " + (p.point_increase >= 0 ? "chip-buy" : "chip-sell")}>
                      {fmtPts(p.point_increase)}
                    </span>
                    <span className="pb-row-rel">{fmtPct(p.relative_increase_pct)}</span>
                    <span className="deal-raw-date">{fmtDate(p.latest_date)}</span>
                  </button>
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

export default function PromoterBuying() {
  const [stocks, setStocks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [selected, setSelected] = useState(null);
  const [sortBy, setSortBy] = useState("latest"); // latest | increase

  function openModal(symbol) {
    setSelected(symbol);
    window.history.pushState({ view: "promoter-buying", modal: "details" }, "");
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
    api.get("/v1/promoter-buying")
      .then((r) => setStocks(r.stocks || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, []);

  const visible = stocks
    .filter((s) =>
      s.symbol.toLowerCase().includes(searchQuery.toLowerCase()) ||
      (s.company_name || "").toLowerCase().includes(searchQuery.toLowerCase())
    )
    .sort((a, b) =>
      sortBy === "latest"
        ? new Date(b.latest_date) - new Date(a.latest_date)
        : b.point_increase - a.point_increase
    );

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          Promoter-Buying Analyser
        </div>
        <div className="row">
          <button className={"chip chip-tab" + (sortBy === "latest" ? " is-active" : "")} onClick={() => setSortBy("latest")}>
            Latest buying
          </button>
          <button className={"chip chip-tab" + (sortBy === "increase" ? " is-active" : "")} onClick={() => setSortBy("increase")}>
            Highest % increase
          </button>
          <input
            className="btn-sm"
            type="text"
            placeholder="Search stock"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
        </div>
      </div>
      {loading ? (
        <SkeletonGrid count={6} lines={3} />
      ) : err ? (
        <div className="err">{err}</div>
      ) : !visible.length ? (
        <div className="empty">No tracked promoter buying yet.</div>
      ) : (
        <div className="promoter-grid">
          {visible.map((s) => (
            <div className="promoter-card" key={s.symbol}>
              <div className="promoter-card-top">
                <div>
                  <span className="promoter-symbol">{s.symbol}</span>
                  <span className="promoter-company">{s.company_name}</span>
                </div>
                <span className={"chip " + (s.point_increase >= 0 ? "chip-buy" : "chip-sell")}>
                  {fmtPts(s.point_increase)}
                </span>
              </div>
              <div className="promoter-meta">
                <div><span className="k">Combined stake</span><span className="v">{s.latest_pct.toFixed(2)}%</span></div>
                <div><span className="k">People tracked</span><span className="v">{s.person_count}</span></div>
                <div><span className="k">Bought</span><span className="v text-green">{fmtVal(s.buy_value)}</span></div>
                <div><span className="k">Sold</span><span className="v text-red">{fmtVal(s.sell_value)}</span></div>
              </div>
              <div className="row" style={{ justifyContent: "space-between", marginTop: 4 }}>
                <span className="subtle" style={{ fontSize: 12 }}>Last disclosure: {fmtDate(s.latest_date)}</span>
                <button className="btn-sm" onClick={() => openModal(s.symbol)}>Details</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {selected && <StockDetailModal symbol={selected} onClose={closeModal} />}
    </div>
  );
}
