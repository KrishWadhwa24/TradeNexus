import React, { useEffect, useState } from "react";
import { api, connectLivePrices, fmt } from "../api.js";

// fraction mirrors internal/paper/intraday.go's marginFraction — kept in
// sync manually since this is just for the live "required margin" preview;
// the server is the actual source of truth and re-validates on submit.
const MARGIN_FRACTION = { DELIVERY: 1, INTRADAY: 0.2 };

// intradayWindowOpen mirrors internal/paper/intraday.go's intradayWindowOpen
// (9:15am-3:20pm IST, weekdays) — a client-side hint only; the server is the
// real source of truth and re-validates on submit regardless.
function intradayWindowOpen() {
  const ist = new Date(new Date().toLocaleString("en-US", { timeZone: "Asia/Kolkata" }));
  const day = ist.getDay();
  if (day === 0 || day === 6) return false;
  const mins = ist.getHours() * 60 + ist.getMinutes();
  return mins >= 9 * 60 + 15 && mins <= 15 * 60 + 20;
}

export default function Trade({ userId }) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState([]);
  const [selected, setSelected] = useState(null); // instrument row
  const [price, setPrice] = useState(null);
  const [priceLoading, setPriceLoading] = useState(false);
  const [qty, setQty] = useState(1);
  const [side, setSide] = useState("BUY");
  const [productType, setProductType] = useState("DELIVERY");
  const [account, setAccount] = useState(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!userId) return;
    api.get(`/v1/users/${userId}/paper/account`).then(setAccount).catch(() => {});
  }, [userId, msg]);

  useEffect(() => {
    if (!msg && !err) return;
    const t = setTimeout(() => { setMsg(""); setErr(""); }, 5000);
    return () => clearTimeout(t);
  }, [msg, err]);

  // Live price ticks over the same WebSocket the rest of the app uses,
  // instead of requiring the user to click Refresh repeatedly.
  useEffect(() => {
    if (!userId || !selected) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (tick.instrument_id === selected.id && tick.price) setPrice(tick.price);
        } catch {
          // Ignore non-tick control messages (heartbeat/ready).
        }
      },
    });
  }, [userId, selected]);

  async function search(e) {
    const v = e.target.value;
    setQ(v);
    if (v.trim().length < 1) { setResults([]); return; }
    try {
      const r = await api.get(`/v1/instruments/search?q=${encodeURIComponent(v)}&limit=12`);
      setResults(r.instruments || []);
    } catch { setResults([]); }
  }

  async function pick(inst) {
    setSelected(inst);
    setResults([]);
    setQ("");
    setErr("");
    setMsg("");
    // Reset to 1 (lot, for an option; share, for an equity) — a leftover
    // value from a previously-selected instrument would be interpreted
    // under the wrong unit otherwise (e.g. "65 shares" carrying over as
    // "65 lots" after switching to an option).
    setQty(1);
    // Short is only valid intraday — if a prior selection had it set,
    // reset to a safe default rather than silently submitting an invalid
    // combination.
    if (side === "SELL" && productType === "DELIVERY") setSide("BUY");
    await refreshPrice(inst);
  }

  async function refreshPrice(inst) {
    const target = inst || selected;
    if (!target) return;
    setPriceLoading(true);
    try {
      const p = await api.get(`/v1/instruments/${target.id}/params`);
      setPrice(p.has_data ? p.price : null);
    } catch {
      setPrice(null);
    } finally {
      setPriceLoading(false);
    }
  }

  function chooseProductType(pt) {
    if (pt === "INTRADAY" && !intradayWindowOpen()) return;
    setProductType(pt);
    if (pt === "DELIVERY" && side === "SELL") setSide("BUY");
  }

  const isOption = selected?.option_type === "CE" || selected?.option_type === "PE";
  const lotSize = isOption ? (selected?.lot_size || 1) : 1;
  // qty is always "how many of the input unit" — lots for an option, plain
  // shares for an equity. actualQuantity is what the backend needs (real
  // units), and what validateOptionLotSize (Step 1) checks server-side.
  const actualQuantity = Number(qty || 0) * lotSize;

  async function submit() {
    if (!selected || !userId) return;
    setBusy(true);
    setErr("");
    setMsg("");
    try {
      const trade = await api.post(`/v1/users/${userId}/paper/trades/open`, {
        instrument_id: selected.id,
        quantity: actualQuantity,
        side,
        product_type: productType,
      });
      setMsg(`${trade.status === "SCHEDULED" ? "Scheduled for next open" : "Opened"}: ${trade.symbol} x${trade.quantity} (${side === "SELL" ? "short" : "long"}, ${productType.toLowerCase()}).`);
      setSelected(null);
      setPrice(null);
      setQty(1);
    } catch (e) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  }

  // An option always costs its full premium — no leverage/margin-financing
  // concept exists for a long option purchase, unlike equity intraday (20%)
  // vs delivery (100%). Mirrors internal/paper/intraday.go's marginFraction
  // (isOption forces 1.0 regardless of productType).
  const marginFraction = isOption ? 1 : MARGIN_FRACTION[productType];
  const margin = price != null ? marginFraction * price * actualQuantity : null;
  const intraOpen = intradayWindowOpen();
  const canShort = productType === "INTRADAY" && intraOpen;
  const canSubmit = selected && price != null && Number(qty) > 0 && !busy && (productType !== "INTRADAY" || intraOpen);

  if (!userId) return <div className="empty">Sign in to trade.</div>;

  return (
    <div>
      <div className="panel" style={{ padding: 18, marginBottom: 22 }}>
        <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: 10 }}>
          <div className="section-title" style={{ margin: 0 }}>Search any stock</div>
          {account && (
            <div style={{ textAlign: "right" }}>
              <div className="subtle">Available cash</div>
              <div style={{ fontSize: 18, fontWeight: 700, fontFamily: "var(--font-mono)" }}>{fmt(account.cash_balance)}</div>
            </div>
          )}
        </div>
        <input
          style={{ width: "100%", maxWidth: 460 }}
          placeholder="Search NSE/BSE stocks (e.g. RELI, TATA)…"
          value={q}
          onChange={search}
        />
        {results.length > 0 && (
          <div className="search-results" style={{ maxWidth: 460 }}>
            {results.map((r) => (
              <div className="search-row" key={r.id}>
                <div><b>{r.trading_symbol}</b> <span className="subtle">{r.name}</span></div>
                <button className="btn-primary btn-sm pill" onClick={() => pick(r)}>Select</button>
              </div>
            ))}
          </div>
        )}
      </div>

      {selected && (
        <div className="panel" style={{ padding: 18 }}>
          <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16 }}>
            <div>
              <div className="section-title" style={{ margin: 0 }}>{selected.trading_symbol}</div>
              <div className="subtle">{selected.name}</div>
            </div>
            <div style={{ textAlign: "right" }}>
              <div className="subtle">Live price</div>
              <div style={{ fontSize: 20, fontWeight: 700 }}>
                {priceLoading ? "…" : price != null ? fmt(price) : "—"}
              </div>
              <button className="btn-sm btn-ghost" onClick={() => refreshPrice()} disabled={priceLoading}>Refresh</button>
            </div>
          </div>

          <div className="row" style={{ gap: 20, flexWrap: "wrap", marginBottom: 16 }}>
            <label style={{ display: "grid", gap: 6 }}>
              <span className="subtle">{isOption ? `Lots (× ${lotSize})` : "Quantity"}</span>
              <input type="number" min="1" step="1" value={qty} onChange={(e) => setQty(e.target.value)} style={{ width: 100 }} />
              {isOption && <span className="subtle" style={{ fontSize: 12 }}>= {actualQuantity} units</span>}
            </label>

            <div style={{ display: "grid", gap: 6 }}>
              <span className="subtle">Product</span>
              <div className="row" style={{ gap: 6 }}>
                <button className={"chip chip-tab" + (productType === "DELIVERY" ? " is-active" : "")} onClick={() => chooseProductType("DELIVERY")}>
                  Delivery
                </button>
                <button
                  className={"chip chip-tab" + (productType === "INTRADAY" ? " is-active" : "")}
                  onClick={() => chooseProductType("INTRADAY")}
                  disabled={!intraOpen}
                  title={intraOpen ? "" : "Orders can be placed only in live market (9:15am-3:20pm IST)"}
                >
                  Intraday
                </button>
              </div>
            </div>

            <div style={{ display: "grid", gap: 6 }}>
              <span className="subtle">Side</span>
              <div className="row" style={{ gap: 6 }}>
                <button className={"chip chip-tab" + (side === "BUY" ? " chip-buy is-active" : "")} onClick={() => setSide("BUY")}>
                  Buy
                </button>
                <button
                  className={"chip chip-tab" + (side === "SELL" ? " chip-sell is-active" : "")}
                  onClick={() => canShort && setSide("SELL")}
                  disabled={!canShort}
                  title={canShort ? "" : "Short selling is only available for Intraday, during live market hours"}
                >
                  Short
                </button>
              </div>
            </div>
          </div>

          {productType === "INTRADAY" && !intraOpen && (
            <div className="err" style={{ marginBottom: 12 }}>
              Orders can be placed only in live market (9:15am-3:20pm IST).
            </div>
          )}
          {productType === "INTRADAY" && intraOpen && (
            <div className="subtle" style={{ marginBottom: 12 }}>
              Intraday positions are auto-closed at 3:20pm IST if not closed or converted to delivery first.
              {side === "SELL" && " Short positions can't be converted to delivery — they must be closed by the cutoff."}
            </div>
          )}

          <div className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <div>
              <span className="subtle">Required margin</span><br />
              <b style={{ fontSize: 18 }}>{margin != null ? fmt(margin) : "—"}</b>
              {productType === "INTRADAY" && <span className="subtle"> (20% · 5x)</span>}
            </div>
            <button className="btn-primary" onClick={submit} disabled={!canSubmit}>
              {busy ? "Placing…" : side === "SELL" ? "Short sell" : "Buy"}
            </button>
          </div>

          {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}
        </div>
      )}

      {msg && <div className="msg" style={{ marginTop: 16 }}>{msg}</div>}
    </div>
  );
}
