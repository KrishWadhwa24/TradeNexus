import React from "react";
import { Icon } from "../icons.jsx";

const OPTIONS = [
  {
    key: "algo-trades",
    label: "Algo Trades",
    icon: "wallet",
    color: "#6a3fd1",
    desc: "Auto-traded option positions, the on/off toggle, and performance.",
  },
  {
    key: "my-option-trades",
    label: "My Option Trades",
    icon: "wallet",
    color: "#d55181",
    desc: "Options you bought manually from the chain.",
  },
  {
    key: "option-chain",
    label: "Option Chain",
    icon: "search",
    color: "#3987e5",
    desc: "Live NIFTY chain — strikes, Greeks, bid/ask — buy directly.",
  },
  {
    key: "option-stats",
    label: "Option Statistics",
    icon: "chart",
    color: "#c98500",
    desc: "Date-range P&L, daily heatmap, and closed algo trades.",
  },
];

// OptionsHub is the "choose a feed" picker page for options trading — same
// pattern as ScannerHub/MarketsHub/AnalyserHub, so this area of the app
// looks and behaves the same as every other multi-view section.
export default function OptionsHub({ onSelect }) {
  return (
    <div>
      <div className="section-title" style={{ marginBottom: 4 }}>Choose a view</div>
      <div className="subtle" style={{ marginBottom: 20 }}>Options trading — algo and manual, in one place.</div>
      <div className="scanner-hub-grid">
        {OPTIONS.map((o) => {
          const I = Icon[o.icon];
          return (
            <button
              key={o.key}
              className="scanner-hub-card"
              style={{ "--icon-color": o.color }}
              onClick={() => onSelect(o.key)}
            >
              <div className="scanner-hub-icon">{I && <I />}</div>
              <div>
                <div className="scanner-hub-name">{o.label}</div>
                <div className="scanner-hub-desc">{o.desc}</div>
              </div>
              <div className="scanner-hub-cta">Open →</div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
