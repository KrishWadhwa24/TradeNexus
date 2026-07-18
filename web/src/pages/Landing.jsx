import React, { useEffect, useRef, useState } from "react";
import { api } from "../api.js";

// Fallback seed used only when the backend can't be reached (e.g. deployed
// frontend with the server offline). When reachable, real data replaces this.
const SEED = [
  { sym: "RELIANCE", name: "Reliance Industries", px: 1421.5 },
  { sym: "TCS", name: "Tata Consultancy Svcs", px: 3208.0 },
  { sym: "HDFCBANK", name: "HDFC Bank", px: 1683.2 },
  { sym: "INFY", name: "Infosys", px: 1552.7 },
  { sym: "ICICIBANK", name: "ICICI Bank", px: 1249.9 },
  { sym: "TATAMOTORS", name: "Tata Motors", px: 722.4 },
  { sym: "BHARTIARTL", name: "Bharti Airtel", px: 1655.0 },
  { sym: "SBIN", name: "State Bank of India", px: 831.6 },
];

const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));
const inr = (n) => (n || 0).toLocaleString("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

// Rows are normalized to { sym, name, px, chg, rsi, dir }. Real data comes from
// the public preview endpoint; a random walk is used only as an offline fallback.
function useLiveStocks() {
  const [rows, setRows] = useState(() =>
    SEED.map((s) => ({ sym: s.sym, name: s.name, px: s.px, open: s.px, chg: 0, rsi: 45 + Math.random() * 20, dir: 0 }))
  );
  const [live, setLive] = useState(false);
  const prevPx = useRef({});

  useEffect(() => {
    let stopped = false;
    let simTimer = null;
    let realTimer = null;

    const applyReal = (data) => {
      const list = (data.rows || []).slice(0, 8).map((p) => {
        const prev = prevPx.current[p.symbol];
        const dir = prev == null ? 0 : Math.sign((p.price || 0) - prev);
        prevPx.current[p.symbol] = p.price;
        return { sym: p.symbol, name: "", px: p.price || 0, chg: p.pct_change || 0, rsi: p.rsi14 || 0, dir };
      });
      if (list.length && !stopped) { setRows(list); setLive(true); }
      return list.length > 0;
    };

    const fetchReal = async () => {
      try {
        const d = await api.get("/v1/public/market-preview?limit=8");
        return applyReal(d);
      } catch { return false; }
    };

    const startSim = () => {
      simTimer = setInterval(() => {
        setRows((prev) =>
          prev.map((r) => {
            const open = r.open || r.px;
            const drift = (Math.random() - 0.48) * r.px * 0.004;
            const px = Math.max(1, r.px + drift);
            const rsi = clamp((r.rsi || 50) + (Math.random() - 0.5) * 5, 32, 82);
            return { ...r, open, px, rsi, dir: Math.sign(px - r.px), chg: ((px - open) / open) * 100 };
          })
        );
      }, 1400);
    };

    (async () => {
      const ok = await fetchReal();
      if (stopped) return;
      if (ok) realTimer = setInterval(fetchReal, 5000);
      else startSim();
    })();

    return () => { stopped = true; if (simTimer) clearInterval(simTimer); if (realTimer) clearInterval(realTimer); };
  }, []);

  return { rows, live };
}

function signalFor(chg, rsi) {
  if (chg > 0.6 && rsi > 55) return "BUY";
  if (chg < -0.6 && rsi < 45) return "SELL";
  return "—";
}

export default function Landing({ onGetStarted }) {
  const { rows, live } = useLiveStocks();
  const [scrolled, setScrolled] = useState(false);
  const dashRef = useRef(null);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 12);
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  const scrollTo = (ref) => ref?.current?.scrollIntoView({ behavior: "smooth", block: "start" });

  const hero = rows[0] || { px: 0, chg: 0, rsi: 0, sym: "—" };
  const heroChg = hero.chg || 0;

  return (
    <div className="lp">
      {/* NAV */}
      <header className={"lp-nav" + (scrolled ? " is-scrolled" : "")}>
        <div className="lp-nav-inner">
          <div className="lp-brand"><span className="prompt">&gt;_</span> Trade<em>Nexus</em></div>
          <nav className="lp-links">
            <a onClick={() => scrollTo(dashRef)}>Live scanner</a>
            <a href="#features">Features</a>
            <a href="#how">How it works</a>
          </nav>
          <div className="lp-nav-cta">
            <button className="lp-ghost" onClick={onGetStarted}>Log in</button>
            <button className="lp-primary" onClick={onGetStarted}>Get started</button>
          </div>
        </div>
      </header>

      {/* HERO */}
      <section className="lp-hero">
        <div className="lp-hero-glow" />
        <div className="lp-hero-grid">
          <div className="lp-hero-copy">
            <span className="lp-kicker">// NSE + BSE · REAL-TIME SCANNER</span>
            <h1 className="lp-title">Catch the move<br />before the <span className="grad">crowd does.</span></h1>
            <p className="lp-lead">
              TradeNexus scans every NSE &amp; BSE stock across daily, weekly and monthly timeframes —
              Chase-Momentum Pine logic, four weekly confluence scanners, and confirmed chart-pattern
              breakouts — then pushes the ones that matter straight to your Telegram.
            </p>
            <div className="lp-cta-row">
              <button className="lp-primary lg" onClick={onGetStarted}>Start scanning free</button>
              <button className="lp-ghost lg" onClick={() => scrollTo(dashRef)}>See it live ↓</button>
            </div>
            <div className="lp-trust">
              <span><b>1,900+</b> stocks tracked</span>
              <span><b>7</b> scanners</span>
              <span><b>Telegram</b> alerts</span>
              <span><b>Paper</b> trading</span>
            </div>
          </div>

          {/* Terminal signal card (echoes a real alert) */}
          <div className="lp-card">
            <div className="lp-card-head">
              <span className="lp-dot g" /><span className="lp-dot a" /><span className="lp-dot r" />
              <span className="lp-card-title">signal · live</span>
            </div>
            <div className="lp-card-body">
              <div className="lp-sig-top">
                <div>
                  <div className="lp-sig-sym">{hero.sym}</div>
                  <div className="lp-sig-strat">Chase Momentum · Daily</div>
                </div>
                <span className="lp-sig-tag">BUY</span>
              </div>
              <div className="lp-sig-price">
                ₹{inr(hero.px)} <span className={heroChg >= 0 ? "up" : "down"}>{heroChg >= 0 ? "▲" : "▼"} {Math.abs(heroChg).toFixed(2)}%</span>
              </div>
              <div className="lp-sig-rows">
                <div><span>Breakout</span><b>20-bar high ✓</b></div>
                <div><span>Rel. volume</span><b>2.1×</b></div>
                <div><span>RSI(14)</span><b>{hero.rsi.toFixed(1)}</b></div>
                <div><span>Trend</span><b>EMA 10&gt;20&gt;40</b></div>
              </div>
              <div className="lp-sig-foot">📈 Delivered to Telegram · 12:41 IST</div>
            </div>
          </div>
        </div>

        {/* Ticker marquee */}
        <div className="lp-ticker">
          <div className="lp-ticker-track">
            {[...rows, ...rows].map((r, i) => (
              <span className="lp-tick" key={i}>
                <b>{r.sym}</b> ₹{inr(r.px)}
                <span className={r.chg >= 0 ? "up" : "down"}>{r.chg >= 0 ? "+" : ""}{(r.chg || 0).toFixed(2)}%</span>
              </span>
            ))}
          </div>
        </div>
      </section>

      {/* LIVE DASHBOARD PREVIEW */}
      <section className="lp-sec" ref={dashRef}>
        <div className="lp-sec-head">
          <span className="lp-kicker">// LIVE PREVIEW</span>
          <h2>Your watchlist, moving in real time.</h2>
          <p>Prices, RSI and signals update the moment the market does — a taste of the dashboard you get inside.</p>
        </div>
        <div className="lp-dash">
          <div className="lp-dash-top">
            <span className="lp-live"><i /> LIVE</span>
            <span className="lp-dash-sub">{live ? "NSE · top movers, live from your server" : "NSE · indicative preview"}</span>
          </div>
          <table className="lp-dash-table">
            <thead>
              <tr><th>Symbol</th><th>Price</th><th>Chg%</th><th>RSI</th><th>Signal</th></tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const chg = r.chg || 0;
                const sig = signalFor(chg, r.rsi);
                return (
                  <tr key={r.sym}>
                    <td><b>{r.sym}</b>{r.name && <span className="lp-name">{r.name}</span>}</td>
                    <td className={"lp-px " + (r.dir > 0 ? "up" : r.dir < 0 ? "down" : "")}>₹{inr(r.px)}</td>
                    <td className={chg >= 0 ? "up" : "down"}>{chg >= 0 ? "+" : ""}{chg.toFixed(2)}%</td>
                    <td>
                      <span className="lp-rsi"><i style={{ width: `${clamp(r.rsi, 0, 100)}%` }} />{r.rsi.toFixed(0)}</span>
                    </td>
                    <td>
                      {sig === "—" ? <span className="muted">—</span>
                        : <span className={"lp-tag " + (sig === "BUY" ? "buy" : "sell")}>{sig}</span>}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      {/* FEATURES */}
      <section className="lp-sec" id="features">
        <div className="lp-sec-head">
          <span className="lp-kicker">// WHAT'S INSIDE</span>
          <h2>Everything to find, verify and act on a setup.</h2>
        </div>
        <div className="lp-feat-grid">
          {FEATURES.map((f) => (
            <div className="lp-feat" key={f.title}>
              <div className="lp-feat-ic">{f.icon}</div>
              <h3>{f.title}</h3>
              <p>{f.desc}</p>
            </div>
          ))}
        </div>
      </section>

      {/* HOW IT WORKS */}
      <section className="lp-sec" id="how">
        <div className="lp-sec-head">
          <span className="lp-kicker">// HOW IT WORKS</span>
          <h2>From scan to Telegram in three steps.</h2>
        </div>
        <div className="lp-steps">
          <div className="lp-step"><span className="lp-step-n">01</span><h3>Build a watchlist</h3><p>Add any NSE/BSE stock. We pull ~5 years of history and keep it in sync automatically.</p></div>
          <div className="lp-step"><span className="lp-step-n">02</span><h3>Scanners run</h3><p>Pine momentum, weekly confluence and confirmed pattern breakouts evaluate every stock, every close.</p></div>
          <div className="lp-step"><span className="lp-step-n">03</span><h3>You get the alert</h3><p>Qualifying signals hit your Telegram with GMP-style conviction, RSI, volume and CMP — then paper-trade them.</p></div>
        </div>
      </section>

      {/* CTA */}
      <section className="lp-cta">
        <div className="lp-cta-inner">
          <h2>Stop watching charts all day.</h2>
          <p>Let the scanners watch for you and ping you when it's worth a look.</p>
          <button className="lp-primary lg" onClick={onGetStarted}>Create your account</button>
        </div>
      </section>

      <footer className="lp-foot">
        <div className="lp-brand"><span className="prompt">&gt;_</span> Trade<em>Nexus</em></div>
        <div className="lp-foot-links">
          <a href="#features">Features</a><a href="#how">How it works</a>
          <a onClick={onGetStarted}>Log in</a>
        </div>
        <div className="lp-foot-copy">© {new Date().getFullYear()} TradeNexus · Not investment advice.</div>
      </footer>
    </div>
  );
}

const FEATURES = [
  { icon: "◈", title: "Pine Chase Momentum", desc: "The full Chase-Momentum Pro strategy replayed on daily, weekly & monthly bars — trend stack, fresh breakout, volume spike and strong-candle confirmation." },
  { icon: "▤", title: "4 weekly confluence scanners", desc: "52-week breakouts, EMA-stack continuation and price-action structure. Confidence is N-of-4, so you see how many independent signals agree." },
  { icon: "◭", title: "Confirmed pattern breakouts", desc: "Cup & handle, rectangle box and downtrend breakouts — only fired on a closed, confirmed breakout candle, not a forming one." },
  { icon: "✈", title: "Telegram alerts", desc: "Rich alerts with conviction, RSI, relative volume and CMP — deduped per stock, timeframe and day, delivered within a 7-day freshness window." },
  { icon: "◐", title: "Paper trading", desc: "Buy any signal on paper, track booked & unbooked P&L, and see which scanners actually make you money before risking real capital." },
  { icon: "🚀", title: "IPO GMP tracker", desc: "Live grey-market premium for every open & upcoming IPO, with an automatic apply signal on the closing day when the premium justifies it." },
];
