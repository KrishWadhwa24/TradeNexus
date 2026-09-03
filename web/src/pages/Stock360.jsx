import React, { useEffect, useState } from "react";
import { api, connectLivePrices, fmt, fmtInt } from "../api.js";

function fmtVal(n) {
  const v = Number(n) || 0;
  const abs = Math.abs(v);
  const sign = v < 0 ? "-" : "";
  if (abs >= 1e7) return sign + "₹" + (abs / 1e7).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " Cr";
  if (abs >= 1e5) return sign + "₹" + (abs / 1e5).toLocaleString("en-IN", { maximumFractionDigits: 2 }) + " L";
  return sign + "₹" + Math.round(abs).toLocaleString("en-IN");
}

function fmtDate(d) {
  if (!d) return "—";
  const t = new Date(d);
  if (isNaN(t.getTime())) return "—";
  return t.toLocaleDateString("en-IN", { day: "2-digit", month: "short", year: "numeric" });
}

function Section({ title, empty, emptyText, children }) {
  return (
    <div style={{ marginBottom: 22 }}>
      <div className="section-title" style={{ marginBottom: 10 }}>{title}</div>
      {empty ? <div className="empty">{emptyText}</div> : <div className="panel">{children}</div>}
    </div>
  );
}

function DealRows({ detail }) {
  const clients = [...(detail.net_buyers || []), ...(detail.net_sellers || [])];
  return (
    <>
      <div className="promoter-meta" style={{ margin: 14 }}>
        <div><span className="k">Buy value</span><span className="v">{fmtVal(detail.buy_value)}</span></div>
        <div><span className="k">Sell value</span><span className="v">{fmtVal(detail.sell_value)}</span></div>
      </div>
      <table>
        <thead><tr><th>Client</th><th>Net Qty</th><th>Net Value</th></tr></thead>
        <tbody>
          {clients.map((c) => (
            <tr key={c.client_name}>
              <td>{c.client_name}</td>
              <td className={c.net_qty >= 0 ? "pos" : "neg"}>{fmtInt(c.net_qty)}</td>
              <td className={c.net_value >= 0 ? "pos" : "neg"}>{fmtVal(c.net_value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}

// Stock360 is the "everything we track about this stock" research page —
// pure aggregation of promoter/insider trades, mutual fund activity, bulk/
// block deals, and scanner signals, all of which already exist elsewhere in
// the app as separate sections. GET /v1/stocks/{id}/360 does the joining
// server-side; this page just renders the result.
export default function Stock360({ userId }) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState([]);
  const [selected, setSelected] = useState(null);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

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
    setData(null);
    setLoading(true);
    try {
      const d = await api.get(`/v1/stocks/${inst.id}/360`);
      setData(d);
    } catch (e) {
      setErr(e.message);
    } finally {
      setLoading(false);
    }
  }

  // Live price ticks over the same WebSocket the rest of the app uses.
  useEffect(() => {
    if (!userId || !selected) return;
    return connectLivePrices(userId, {
      onMessage: (event) => {
        try {
          const tick = JSON.parse(event.data);
          if (tick.instrument_id === selected.id && tick.price) {
            setData((cur) => cur ? { ...cur, price: { ...cur.price, price: tick.price, has_data: true } } : cur);
          }
        } catch {
          // Ignore non-tick control messages (heartbeat/ready).
        }
      },
    });
  }, [userId, selected]);

  return (
    <div>
      <div className="panel" style={{ padding: 18, marginBottom: 22 }}>
        <div className="section-title" style={{ margin: "0 0 10px" }}>Search any stock</div>
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

      {loading && <div className="spinner">Loading…</div>}
      {err && <div className="err">{err}</div>}
      {!selected && !loading && <div className="empty">Search a stock above to see everything tracked about it.</div>}

      {data && (
        <>
          <div className="panel" style={{ padding: 18, marginBottom: 22 }}>
            <div className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
              <div>
                <div className="section-title" style={{ margin: 0 }}>{data.symbol}</div>
                <div className="subtle">{data.company_name}</div>
              </div>
              <div style={{ textAlign: "right" }}>
                <div className="subtle">Live price</div>
                <div style={{ fontSize: 22, fontWeight: 700, fontFamily: "var(--font-mono)" }}>
                  {data.price?.has_data ? fmt(data.price.price) : "—"}
                </div>
                {data.price?.has_data && (
                  <div className={data.price.pct_change >= 0 ? "pos" : "neg"}>
                    {data.price.pct_change >= 0 ? "+" : ""}{data.price.pct_change.toFixed(2)}%
                  </div>
                )}
              </div>
            </div>
          </div>

          <Section title="Promoter & Insider Trades" empty={!data.promoters?.length} emptyText="No tracked promoter/insider activity for this stock.">
            <table>
              <thead><tr><th>Person</th><th>Category</th><th>Stake move</th><th>Point Δ</th><th>Bought</th><th>Sold</th></tr></thead>
              <tbody>
                {(data.promoters || []).map((p) => (
                  <tr key={p.person_name}>
                    <td>{p.person_name}</td>
                    <td>{p.category}</td>
                    <td>{p.first_pct.toFixed(2)}% → {p.latest_pct.toFixed(2)}%</td>
                    <td className={p.point_increase >= 0 ? "pos" : "neg"}>{p.point_increase >= 0 ? "+" : ""}{p.point_increase.toFixed(2)}</td>
                    <td>{fmtInt(p.buy_qty)}</td>
                    <td>{fmtInt(p.sell_qty)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>

          <Section title="Mutual Fund Activity" empty={!data.funds?.length} emptyText="No tracked mutual fund activity for this stock.">
            <table>
              <thead><tr><th>Fund</th><th>Bought</th><th>Sold</th><th>Net value</th><th>Deals</th><th>Last deal</th></tr></thead>
              <tbody>
                {(data.funds || []).map((f) => (
                  <tr key={f.fund_name}>
                    <td>{f.fund_name}</td>
                    <td>{fmtInt(f.buy_qty)}</td>
                    <td>{fmtInt(f.sell_qty)}</td>
                    <td className={f.net_value >= 0 ? "pos" : "neg"}>{fmtVal(f.net_value)}</td>
                    <td>{f.deal_count}</td>
                    <td>{fmtDate(f.last_deal_date)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>

          <Section title="Big Investor Holdings" empty={!data.big_investors?.length} emptyText="No tracked big investor currently holds a disclosed stake in this stock.">
            <table>
              <thead><tr><th>Investor</th><th>Holding %</th><th>Shares</th><th>As of</th></tr></thead>
              <tbody>
                {(data.big_investors || []).map((h) => (
                  <tr key={h.investor_name}>
                    <td>{h.investor_name}</td>
                    <td>{h.pct_holding.toFixed(2)}%</td>
                    <td>{fmtInt(h.shares)}</td>
                    <td>{fmtDate(h.report_date)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>

          <Section title="Bulk Deals (last 30 days)" empty={!data.bulk_deals?.rows?.length} emptyText="No bulk deals in the last 30 days.">
            <DealRows detail={data.bulk_deals} />
          </Section>

          <Section title="Block Deals (last 30 days)" empty={!data.block_deals?.rows?.length} emptyText="No block deals in the last 30 days.">
            <DealRows detail={data.block_deals} />
          </Section>

          <Section title="Scanner Signals" empty={!data.signals?.length} emptyText="No recent scanner signals for this stock.">
            <table>
              <thead><tr><th>Scanner</th><th>Timeframe</th><th>Direction</th><th>Date</th><th>Price</th></tr></thead>
              <tbody>
                {(data.signals || []).map((s) => (
                  <tr key={s.id}>
                    <td>{s.scanner_name}</td>
                    <td>{s.timeframe}</td>
                    <td><span className={"tag " + (s.direction === "BUY" ? "tag-buy" : "tag-sell")}>{s.direction}</span></td>
                    <td>{fmtDate(s.candle_date)}</td>
                    <td>{s.price ? fmt(s.price) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>
        </>
      )}
    </div>
  );
}
