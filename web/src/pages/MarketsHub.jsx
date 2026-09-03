import React from "react";
import { Icon } from "../icons.jsx";

const MARKETS = [
  {
    key: "ipo",
    label: "IPO Tracker",
    icon: "rocket",
    color: "#6a3fd1",
    desc: "Open and upcoming IPOs, with GMP signals as they come in.",
  },
  {
    key: "promoter",
    label: "Promoter Trades",
    icon: "pulse",
    color: "#d55181",
    desc: "Live feed of promoter and director/KMP market buys and sells.",
  },
  {
    key: "bulk",
    label: "Bulk Deals",
    icon: "list",
    color: "#c98500",
    desc: "Large single-trade blocks reported by the exchange.",
  },
  {
    key: "block",
    label: "Block Deals",
    icon: "list",
    color: "#3987e5",
    desc: "Privately negotiated large trades executed on-exchange.",
  },
];

// MarketsHub is the desktop "choose a feed" landing page — same picker
// pattern as ScannerHub/AnalyserHub. Mobile skips this entirely: these four
// feeds already have their own bottom-nav tabs there (see App.jsx's
// mobileHidden on the "markets" nav entry and SWIPE_TABS), so a picker page
// would just be a redundant extra tap on a phone.
export default function MarketsHub({ onSelect }) {
  return (
    <div>
      <div className="section-title" style={{ marginBottom: 4 }}>Choose a feed</div>
      <div className="subtle" style={{ marginBottom: 20 }}>Raw market activity, straight from the exchange.</div>
      <div className="scanner-hub-grid">
        {MARKETS.map((m) => {
          const I = Icon[m.icon];
          return (
            <button
              key={m.key}
              className="scanner-hub-card"
              style={{ "--icon-color": m.color }}
              onClick={() => onSelect(m.key)}
            >
              <div className="scanner-hub-icon">{I && <I />}</div>
              <div>
                <div className="scanner-hub-name">{m.label}</div>
                <div className="scanner-hub-desc">{m.desc}</div>
              </div>
              <div className="scanner-hub-cta">Open feed →</div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
