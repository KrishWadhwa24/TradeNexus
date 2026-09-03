import React, { useEffect } from "react";
import IPO from "./IPO.jsx";
import PromoterTrades from "./PromoterTrades.jsx";
import Deals from "./Deals.jsx";
import { Icon } from "../icons";

// Per-view <title>/description — the whole point of these routes existing
// is to be indexable and shareable, so each one gets its own, not the
// static default from index.html.
const META = {
  ipo: {
    title: "IPO Tracker — live GMP & subscription",
    desc: "Live NSE/BSE IPO grey market premium, subscription numbers, and listing dates.",
  },
  "promoter-trades": {
    title: "Promoter Buying — NSE PIT feed",
    desc: "Every promoter and director/KMP market buy and sell disclosed on the NSE PIT feed.",
  },
  bulk: {
    title: "Bulk Deals — NSE feed",
    desc: "NSE bulk deal disclosures — net buyers and sellers by stock, net ≥ ₹5 Cr.",
  },
  block: {
    title: "Block Deals — NSE feed",
    desc: "NSE block deal disclosures — net buyers and sellers by stock.",
  },
};

// PublicShell renders one of the no-login sections (IPO / Promoter Buying /
// Bulk / Block Deals) in a slim marketing-site shell instead of the full
// authenticated dashboard — the landing spot for a shared link or a search
// result, not something meant to feel like the logged-in app.
export default function PublicShell({ view, symbol, onGetStarted, theme, onToggleTheme }) {
  const meta = META[view] || {};

  useEffect(() => {
    document.title = meta.title ? `${meta.title} — TradeNexus` : "TradeNexus";
    const tag = document.querySelector('meta[name="description"]');
    if (tag && meta.desc) tag.setAttribute("content", meta.desc);
  }, [meta.title, meta.desc]);

  return (
    <div className="lp">
      <header className="lp-nav is-scrolled">
        <div className="lp-nav-inner">
          <div className="lp-brand" style={{ cursor: "pointer" }} onClick={() => { window.location.href = "/"; }}>
            <span className="prompt">&gt;_</span> Trade<em>Nexus</em>
          </div>
          <div className="lp-nav-cta">
            <button className="icon-btn" title="Toggle theme" onClick={onToggleTheme}>
              {theme === "dark" ? <Icon.sun /> : <Icon.moon />}
            </button>
            <button className="lp-ghost" onClick={onGetStarted}>Log in</button>
            <button className="lp-primary" onClick={onGetStarted}>Get started</button>
          </div>
        </div>
      </header>

      <div className="content" style={{ margin: "0 auto" }}>
        {view === "ipo" && <IPO initialName={symbol} />}
        {view === "promoter-trades" && <PromoterTrades initialSymbol={symbol} publicView />}
        {view === "bulk" && <Deals type="bulk" initialSymbol={symbol} />}
        {view === "block" && <Deals type="block" initialSymbol={symbol} />}
      </div>

      <div className="public-cta">
        <div>
          <b>Want watchlists, scanners, paper trading, and Telegram alerts too?</b>
          <div className="subtle">Everything above is free to browse — sign up for the rest.</div>
        </div>
        <button className="lp-primary lg" onClick={onGetStarted}>Create your free account</button>
      </div>
    </div>
  );
}
