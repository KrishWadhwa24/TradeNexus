import React, { useCallback, useEffect, useState } from "react";
import { api, connectLivePrices, fmt } from "../api.js";

function Stat({ label, value, cls }) {
  return (
    <div className="card">
      <div className="label">{label}</div>
      <div className={"value " + (cls || "")}>{value}</div>
    </div>
  );
}

// unrealizedPnL mirrors internal/paper/intraday.go's unrealizedPnL exactly
// — a short profits when price falls, a long when it rises.
function unrealizedPnL(side, entryPrice, currentPrice, qty) {
  return side === "SELL" ? (entryPrice - currentPrice) * qty : (currentPrice - entryPrice) * qty;
}

// marginFraction mirrors internal/paper/intraday.go's marginFraction.
function marginFraction(productType) {
  return productType === "INTRADAY" ? 0.2 : 1;
}

function productBadge(t) {
  if (t.product_type !== "INTRADAY") return "Delivery";
  return t.side === "SELL" ? "Intraday · Short" : "Intraday · Long";
}

// PositionCard is one held position (possibly the average of several
// merged buys — see internal/paper's mergeOrOpen). The qty input lets the
// user sell any amount up to what's held, defaulting to the full position.
// A pending close (market's shut) doesn't change this card at all — it
// shows up as its own row in the Pending tab instead, same as a pending buy.
function PositionCard({ t, onSell, onConvert }) {
  const [sellQty, setSellQty] = useState(t.quantity);
  useEffect(() => { setSellQty(t.quantity); }, [t.quantity]);

  const canConvert = t.product_type === "INTRADAY" && t.side === "BUY";
  const invested = marginFraction(t.product_type) * t.entry_price * t.quantity;
  const pnlCls = t.unrealized_pnl >= 0 ? "text-green" : "text-red";
  const exitLabel = t.side === "SELL" ? "Cover" : "Sell";

  return (
    <div className="promoter-card">
      <div className="promoter-card-top">
        <span className="promoter-symbol">{t.symbol}</span>
        <span style={{ fontWeight: 700, fontFamily: "var(--font-mono)" }} title="Live price">{fmt(t.current_price)}</span>
      </div>

      <div className="promoter-meta">
        <div><span className="k">Quantity</span><span className="v">{t.quantity}</span></div>
        <div><span className="k">Avg price</span><span className="v">{fmt(t.entry_price)}</span></div>
        <div><span className="k">Invested</span><span className="v">{fmt(invested)}</span></div>
        <div><span className="k">Profit</span><span className={"v " + pnlCls}>{fmt(t.unrealized_pnl)}</span></div>
      </div>

      <div className="promoter-foot">
        <div className="row" style={{ gap: 6 }}>
          <input
            type="number" min="1" max={t.quantity} value={sellQty}
            onChange={(e) => setSellQty(e.target.value)}
            style={{ width: 64 }}
          />
          <button className="btn-sm" onClick={() => onSell(t.id, Number(sellQty))}>{exitLabel}</button>
        </div>
        {canConvert && (
          <button className="btn-sm btn-ghost" onClick={() => onConvert(t.id)} title="Pay the remaining 80% to hold as a normal delivery position">
            Convert
          </button>
        )}
      </div>
    </div>
  );
}

// PendingCard covers two kinds of not-yet-executed orders: a SCHEDULED buy
// (no position yet) and an OPEN position with a pending close (still held,
// but queued to sell/cover at next open) — both just "fills at next market
// open, cancellable", so they share this one card shape.
function PendingCard({ t, onCancel }) {
  const isPendingClose = t.status === "OPEN" && t.pending_close_qty;
  const label = isPendingClose
    ? `${t.pending_close_qty} sh · ${t.side === "SELL" ? "Cover" : "Sell"} pending`
    : `${t.quantity} sh · ${productBadge(t)}`;
  return (
    <div className="promoter-card">
      <div className="promoter-card-top">
        <div>
          <span className="promoter-symbol">{t.symbol}</span>
          <span className="position-sub">{label}</span>
        </div>
      </div>
      <div className="promoter-foot">
        <span className="subtle">Fills at next market open</span>
        <button className="btn-sm btn-ghost" onClick={() => onCancel(t.id)} title="Cancel this order before it fills">
          Cancel
        </button>
      </div>
    </div>
  );
}

const TABS = [
  { key: "delivery", label: "Delivery" },
  { key: "intraday-long", label: "Intraday · Long" },
  { key: "intraday-short", label: "Intraday · Short" },
  { key: "pending", label: "Pending" },
  { key: "history", label: "History" },
];

function tabFilter(key) {
  switch (key) {
    case "delivery": return (t) => t.status === "OPEN" && t.product_type === "DELIVERY";
    case "intraday-long": return (t) => t.status === "OPEN" && t.product_type === "INTRADAY" && t.side === "BUY";
    case "intraday-short": return (t) => t.status === "OPEN" && t.product_type === "INTRADAY" && t.side === "SELL";
    case "pending": return (t) => t.status === "SCHEDULED" || (t.status === "OPEN" && t.pending_close_qty);
    case "history": return (t) => t.status === "CLOSED" || t.status === "CANCELLED";
    default: return () => false;
  }
}

export default function Paper({ userId }) {
  const [sum, setSum] = useState(null);
  const [trades, setTrades] = useState([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [tab, setTab] = useState("delivery");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/summary`),
      api.get(`/v1/users/${userId}/paper/trades`),
    ])
      .then(([s, t]) => { setSum(s); setTrades(t.trades || []); })
      .catch((e) => setErr(e.message));
  }, [userId]);

  useEffect(() => {
    if (!msg) return;
    const t = setTimeout(() => setMsg(""), 5000);
    return () => clearTimeout(t);
  }, [msg]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    if (!userId) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (!tick.instrument_id || !tick.price) return;
          setTrades((cur) => cur.map((t) => {
            if (t.status !== "OPEN" || t.instrument_id !== tick.instrument_id) return t;
            return { ...t, current_price: tick.price, unrealized_pnl: unrealizedPnL(t.side, t.entry_price, tick.price, t.quantity) };
          }));
        } catch {
          // Ignore non-tick control messages (heartbeat/ready).
        }
      },
    });
  }, [userId]);

  async function sell(id, qty) {
    try {
      const trade = await api.post(`/v1/paper/trades/${id}/close`, { quantity: qty });
      setMsg(trade.pending_close_qty ? "Market's closed — scheduled to sell at next open." : "Order executed.");
      load();
    } catch (e) { setMsg("Failed: " + e.message); }
  }

  async function convert(id) {
    try {
      await api.post(`/v1/paper/trades/${id}/convert`, {});
      setMsg("Converted to delivery.");
      load();
    } catch (e) { setMsg("Convert failed: " + e.message); }
  }

  async function cancel(id) {
    try {
      await api.post(`/v1/paper/trades/${id}/cancel`, {});
      setMsg("Order cancelled.");
      load();
    } catch (e) { setMsg("Cancel failed: " + e.message); }
  }

  if (!userId) return <div className="empty">Select a user to view paper trading.</div>;
  if (err) return <div className="err">{err}</div>;
  if (!sum) return <div className="spinner">Loading…</div>;

  // Market value/equity generalize to margin positions the same way the
  // backend's Summary does: for each open row, "what would come back to
  // cash if closed now" = marginFraction*entryPrice*qty + unrealized_pnl.
  const openTrades = trades.filter((t) => t.status === "OPEN");
  const marketValue = openTrades.reduce((s, t) => s + marginFraction(t.product_type) * t.entry_price * t.quantity + t.unrealized_pnl, 0);
  const unrealizedPnlTotal = openTrades.reduce((s, t) => s + t.unrealized_pnl, 0);
  const totalPnl = sum.realized_pnl + unrealizedPnlTotal;
  const equity = sum.cash_balance + marketValue;
  const pnlCls = totalPnl >= 0 ? "pos" : "neg";

  const counts = Object.fromEntries(TABS.map((tb) => [tb.key, trades.filter(tabFilter(tb.key)).length]));
  const visible = trades.filter(tabFilter(tab));

  return (
    <div>
      <div className="grid cards" style={{ marginBottom: 22 }}>
        <Stat label="Invested (open)" value={fmt(sum.invested)} />
        <Stat label="Market value" value={fmt(marketValue)} />
        <Stat label="Unrealized P&L" value={fmt(unrealizedPnlTotal)} cls={unrealizedPnlTotal >= 0 ? "pos" : "neg"} />
        <Stat label="Total P&L" value={fmt(totalPnl)} cls={pnlCls} />
        <Stat label="Cash" value={fmt(sum.cash_balance)} />
        <Stat label="Equity" value={fmt(equity)} />
      </div>

      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>Positions & trade history</div>
        <div className="row">
          {msg && <span className="msg">{msg}</span>}
          <button className="btn-sm" onClick={load}>Refresh</button>
        </div>
      </div>

      <div className="promoter-filters" style={{ marginBottom: 16 }}>
        {TABS.map((tb) => (
          <button
            key={tb.key}
            className={"chip chip-tab" + (tab === tb.key ? " is-active" : "")}
            onClick={() => setTab(tb.key)}
          >
            {tb.label} <small>{counts[tb.key]}</small>
          </button>
        ))}
      </div>

      {!visible.length ? (
        <div className="empty">
          {tab === "history" ? "No closed or cancelled trades yet." : "Nothing here yet. Search a stock in the Trade tab, or buy from the Scanner tab."}
        </div>
      ) : tab === "history" ? (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>Symbol</th><th>Type</th><th>Status</th><th>Qty</th><th>Entry</th><th>Exit</th><th>P&L</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((t) => (
                <tr key={t.id}>
                  <td>{t.symbol}</td>
                  <td><span className="tag">{productBadge(t)}</span></td>
                  <td><span className="tag">{t.status}</span></td>
                  <td>{t.quantity}</td>
                  <td>{t.entry_price ? fmt(t.entry_price) : "—"}</td>
                  <td>{t.exit_price ? fmt(t.exit_price) : "—"}</td>
                  <td className={t.pnl >= 0 ? "pos" : "neg"}>{fmt(t.pnl)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : tab === "pending" ? (
        <div className="promoter-grid">
          {visible.map((t) => <PendingCard key={t.id} t={t} onCancel={cancel} />)}
        </div>
      ) : (
        <div className="promoter-grid">
          {visible.map((t) => <PositionCard key={t.id} t={t} onSell={sell} onConvert={convert} />)}
        </div>
      )}
    </div>
  );
}
