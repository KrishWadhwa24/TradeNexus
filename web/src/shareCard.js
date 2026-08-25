// Shareable "stock card" — a branded portrait image summarizing one stock's
// live metrics (price, RSI, EMAs, volume, ...), built to be shared to
// WhatsApp/Telegram/Gmail/etc. Same canvas → toBlob → download recipe as the
// FII/DII, Mutual Fund and Promoter Buying "download as image" features
// (see Insights.jsx/MutualFunds.jsx/PromoterBuying.jsx), pulled out here
// since this one is meant to be reused across multiple pages, not just one.

const HEX = {
  bg: "#0d0f13",
  border: "#1d2129",
  brand: "#8b8bff",
  text: "#e8eaed",
  muted: "#7d8590",
  green: "#3ecf8e",
  red: "#f0616d",
};

const STATS = [
  ["RSI (14)", (p) => p.rsi14?.toFixed(1)],
  ["ATR (14)", (p) => p.atr14?.toFixed(2)],
  ["EMA 10", (p) => p.ema10?.toFixed(2)],
  ["EMA 20", (p) => p.ema20?.toFixed(2)],
  ["EMA 50", (p) => p.ema50?.toFixed(2)],
  ["SMA 40", (p) => p.sma40?.toFixed(2)],
  ["Volume", (p) => Math.round(p.volume || 0).toLocaleString("en-IN")],
  ["Vol SMA20", (p) => Math.round(p.vol_sma20 || 0).toLocaleString("en-IN")],
];

function roundRect(ctx, x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

// buildStockCardBlob draws the card for one stock's `Params`-shaped object
// (symbol, price, pct_change, rsi14, ema10/20/50, sma40, atr14, volume,
// vol_sma20 — exactly what GET /v1/users/{uid}/dashboard already returns
// per row) and resolves to a PNG Blob.
export async function buildStockCardBlob(p) {
  await document.fonts.ready;

  const up = (p.pct_change || 0) >= 0;
  const accent = up ? HEX.green : HEX.red;
  const scale = 2, W = 480, H = 620;
  const canvas = document.createElement("canvas");
  canvas.width = W * scale;
  canvas.height = H * scale;
  const ctx = canvas.getContext("2d");
  ctx.scale(scale, scale);

  // Card body + accent-colored frame (the "Pokemon card" border).
  roundRect(ctx, 0, 0, W, H, 22);
  ctx.fillStyle = HEX.bg;
  ctx.fill();
  ctx.lineWidth = 3;
  ctx.strokeStyle = accent;
  roundRect(ctx, 1.5, 1.5, W - 3, H - 3, 21);
  ctx.stroke();

  ctx.fillStyle = HEX.brand;
  ctx.font = "700 15px 'Space Grotesk', sans-serif";
  ctx.fillText(">_ TradeNexus", 28, 42);
  ctx.textAlign = "right";
  ctx.fillStyle = HEX.muted;
  ctx.font = "600 11px 'JetBrains Mono', monospace";
  ctx.fillText("STOCK CARD", W - 28, 42);
  ctx.textAlign = "left";

  ctx.fillStyle = HEX.text;
  ctx.font = "700 34px 'Space Grotesk', sans-serif";
  ctx.fillText(p.symbol, 28, 100);

  ctx.font = "700 30px 'JetBrains Mono', monospace";
  ctx.fillText("₹" + (p.price?.toFixed(2) ?? "—"), 28, 148);

  const chg = `${up ? "+" : ""}${(p.pct_change || 0).toFixed(2)}%`;
  ctx.font = "700 16px 'JetBrains Mono', monospace";
  const chgW = ctx.measureText(chg).width + 24;
  roundRect(ctx, W - 28 - chgW, 122, chgW, 32, 8);
  ctx.fillStyle = up ? "rgba(62,207,142,.14)" : "rgba(240,97,109,.14)";
  ctx.fill();
  ctx.fillStyle = accent;
  ctx.fillText(chg, W - 28 - chgW + 12, 144);

  ctx.strokeStyle = HEX.border;
  ctx.beginPath();
  ctx.moveTo(28, 178);
  ctx.lineTo(W - 28, 178);
  ctx.stroke();

  // Stat grid — 2 columns x 4 rows, like a trading-card stat block.
  const cellW = (W - 56) / 2, cellH = 92, gridTop = 204;
  STATS.forEach(([label, get], i) => {
    const col = i % 2, row = Math.floor(i / 2);
    const x = 28 + col * cellW, y = gridTop + row * cellH;
    ctx.fillStyle = HEX.muted;
    ctx.font = "600 11px 'JetBrains Mono', monospace";
    ctx.fillText(label.toUpperCase(), x, y);
    ctx.fillStyle = HEX.text;
    ctx.font = "700 22px 'Space Grotesk', sans-serif";
    ctx.fillText(get(p) ?? "—", x, y + 30);
  });

  ctx.fillStyle = HEX.muted;
  ctx.font = "400 11px 'JetBrains Mono', monospace";
  ctx.fillText(`Generated ${new Date().toLocaleString("en-IN")}`, 28, H - 24);

  return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
}

function gmpAccent(pct) {
  if (pct >= 20) return HEX.green;
  if (pct >= 10) return "#e3a008"; // matches .conv-text-med / --amber elsewhere in the app
  return HEX.muted;
}

function fmtIpoDate(d) {
  if (!d) return "—";
  const t = new Date(String(d).slice(0, 10) + "T00:00:00");
  return isNaN(t.getTime()) ? String(d).slice(0, 10) : t.toLocaleDateString("en-IN", { day: "2-digit", month: "short" });
}

const IPO_STATS = [
  ["Price", (x) => (x.price ? "₹" + x.price : "—")],
  ["Lot size", (x) => x.lot || "—"],
  ["Subscription", (x) => (x.subscription && x.subscription !== "-" ? x.subscription : "—")],
  ["IPO size", (x) => x.ipo_size || "—"],
  ["QIB sub", (x) => (x.qib ? x.qib.toFixed(2) + "x" : "—")],
  ["Rating", (x) => (x.rating > 0 ? "🔥".repeat(x.rating) : "—")],
];

// buildIpoCardBlob draws the card for one IPO.jsx row (name, board/category,
// status, rating, gmp/gmp_percent, price, lot, subscription, ipo_size, qib,
// open/close/listing dates — exactly what GET /v1/ipos already returns).
export async function buildIpoCardBlob(x) {
  await document.fonts.ready;

  const hasGmp = x.gmp > 0 || x.gmp_percent > 0;
  const accent = hasGmp ? gmpAccent(x.gmp_percent) : HEX.muted;
  const scale = 2, W = 480, H = 640;
  const canvas = document.createElement("canvas");
  canvas.width = W * scale;
  canvas.height = H * scale;
  const ctx = canvas.getContext("2d");
  ctx.scale(scale, scale);

  roundRect(ctx, 0, 0, W, H, 22);
  ctx.fillStyle = HEX.bg;
  ctx.fill();
  ctx.lineWidth = 3;
  ctx.strokeStyle = accent;
  roundRect(ctx, 1.5, 1.5, W - 3, H - 3, 21);
  ctx.stroke();

  ctx.fillStyle = HEX.brand;
  ctx.font = "700 15px 'Space Grotesk', sans-serif";
  ctx.fillText(">_ TradeNexus", 28, 42);
  ctx.textAlign = "right";
  ctx.fillStyle = HEX.muted;
  ctx.font = "600 11px 'JetBrains Mono', monospace";
  ctx.fillText("IPO CARD", W - 28, 42);
  ctx.textAlign = "left";

  ctx.fillStyle = HEX.text;
  ctx.font = "700 28px 'Space Grotesk', sans-serif";
  const name = x.name.length > 24 ? x.name.slice(0, 23) + "…" : x.name;
  ctx.fillText(name, 28, 92);

  ctx.fillStyle = HEX.muted;
  ctx.font = "600 12px 'JetBrains Mono', monospace";
  ctx.fillText(`${(x.board || x.category || "").toUpperCase()} · ${(x.status || "").toUpperCase()}`, 28, 114);

  // GMP block, like ipo-gmp-block in the app UI.
  roundRect(ctx, 28, 136, W - 56, 68, 10);
  ctx.fillStyle = "#15181d";
  ctx.fill();
  ctx.fillStyle = HEX.muted;
  ctx.font = "600 10.5px 'JetBrains Mono', monospace";
  ctx.fillText("GMP", 46, 162);
  ctx.fillStyle = accent;
  ctx.font = "700 24px 'JetBrains Mono', monospace";
  ctx.fillText(hasGmp ? `₹${x.gmp} (${x.gmp_percent}%)` : "—", 46, 190);

  const lot = parseInt(x.lot, 10);
  if (hasGmp && lot && !isNaN(lot)) {
    const profit = "₹" + (x.gmp * lot).toLocaleString("en-IN");
    ctx.textAlign = "right";
    ctx.fillStyle = HEX.muted;
    ctx.font = "600 10.5px 'JetBrains Mono', monospace";
    ctx.fillText("PROFIT / LOT", W - 46, 162);
    ctx.fillStyle = HEX.text;
    ctx.font = "700 20px 'JetBrains Mono', monospace";
    ctx.fillText(profit, W - 46, 190);
    ctx.textAlign = "left";
  }

  // Stat grid — 2 columns x 3 rows.
  const cellW = (W - 56) / 2, cellH = 78, gridTop = 246;
  IPO_STATS.forEach(([label, get], i) => {
    const col = i % 2, row = Math.floor(i / 2);
    const x0 = 28 + col * cellW, y = gridTop + row * cellH;
    ctx.fillStyle = HEX.muted;
    ctx.font = "600 11px 'JetBrains Mono', monospace";
    ctx.fillText(label.toUpperCase(), x0, y);
    ctx.fillStyle = HEX.text;
    ctx.font = "700 19px 'Space Grotesk', sans-serif";
    ctx.fillText(String(get(x)), x0, y + 26);
  });

  ctx.strokeStyle = HEX.border;
  ctx.beginPath();
  ctx.moveTo(28, gridTop + 3 * cellH - 30);
  ctx.lineTo(W - 28, gridTop + 3 * cellH - 30);
  ctx.stroke();

  // Dates row.
  const dateY = gridTop + 3 * cellH;
  const dateColW = (W - 56) / 3;
  [["Open", x.open_date], ["Close", x.close_date], ["Lists", x.listing_date]].forEach(([label, d], i) => {
    const x0 = 28 + i * dateColW;
    ctx.fillStyle = HEX.muted;
    ctx.font = "600 10.5px 'JetBrains Mono', monospace";
    ctx.fillText(label.toUpperCase(), x0, dateY);
    ctx.fillStyle = HEX.text;
    ctx.font = "700 15px 'Space Grotesk', sans-serif";
    ctx.fillText(fmtIpoDate(d), x0, dateY + 22);
  });

  ctx.fillStyle = HEX.muted;
  ctx.font = "400 11px 'JetBrains Mono', monospace";
  ctx.fillText(`Generated ${new Date().toLocaleString("en-IN")}`, 28, H - 24);

  return new Promise((resolve) => canvas.toBlob(resolve, "image/png"));
}

export function ipoShareCaption(x) {
  const hasGmp = x.gmp > 0 || x.gmp_percent > 0;
  const gmpText = hasGmp ? `GMP ₹${x.gmp} (${x.gmp_percent}%)` : "GMP —";
  return `${x.name} IPO — ${gmpText} · ${x.status} — via TradeNexus`;
}

export async function shareIpoCard(x) {
  const blob = await buildIpoCardBlob(x);
  return shareCard({
    blob,
    filename: `${x.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}-ipo.png`,
    caption: ipoShareCaption(x),
    title: `${x.name} IPO — TradeNexus`,
  });
}

export function shareCaption(p) {
  const up = (p.pct_change || 0) >= 0;
  const chg = `${up ? "+" : ""}${(p.pct_change || 0).toFixed(2)}%`;
  return `${p.symbol} — ₹${p.price?.toFixed(2) ?? "—"} (${chg}) · RSI ${p.rsi14?.toFixed(1) ?? "—"} — via TradeNexus`;
}

export function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

// shareCard is the reusable orchestrator behind every "Share" button: hand
// the image straight to the OS-level share sheet ("share to WhatsApp/
// Telegram/Gmail/...") on browsers that support sharing files — this needs
// a secure context (https, or localhost) or `navigator.share` won't exist
// at all — or, where that's unsupported (most desktop browsers, or any
// plain-http origin like a phone hitting a LAN dev server), download the
// image and return the caption so the caller can offer text-based share
// links as a fallback (see ShareButton.jsx).
export async function shareCard({ blob, filename, caption, title }) {
  const file = new File([blob], filename, { type: "image/png" });

  if (navigator.share && (!navigator.canShare || navigator.canShare({ files: [file] }))) {
    try {
      await navigator.share({ files: [file], title, text: caption });
      return { shared: true };
    } catch (e) {
      if (e?.name === "AbortError") return { shared: false, cancelled: true };
      // fall through to the manual fallback below
    }
  }

  downloadBlob(blob, filename);
  return { shared: false, downloaded: true, caption };
}

// shareStockCard: build + share a stock card in one call (used by pages
// that don't need the intermediate blob/caption for anything else).
export async function shareStockCard(p) {
  const blob = await buildStockCardBlob(p);
  return shareCard({
    blob,
    filename: `${(p.symbol || "stock").toLowerCase()}-card.png`,
    caption: shareCaption(p),
    title: `${p.symbol} — TradeNexus`,
  });
}
