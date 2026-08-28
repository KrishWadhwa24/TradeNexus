import React from "react";
import { Icon } from "../icons.jsx";

const ANALYSERS = [
  {
    key: "mutual-funds",
    label: "Mutual-Funds",
    icon: "wallet",
    color: "#199e70",
    desc: "Which mutual funds are buying and selling which stocks, ranked by conviction.",
  },
  {
    key: "promoter-buying",
    label: "Promoter Analyser",
    icon: "trending",
    color: "#3987e5",
    desc: "Promoter and insider stake changes, ranked by combined point-increase.",
  },
];

// AnalyserHub is the "choose an analyser" landing page — same picker
// pattern as ScannerHub, for the same reason: a 2+ item sidebar submenu
// becomes a proper landing page instead.
export default function AnalyserHub({ onSelect }) {
  return (
    <div>
      <div className="section-title" style={{ marginBottom: 4 }}>Choose an analyser</div>
      <div className="subtle" style={{ marginBottom: 20 }}>Pick a derived/aggregated view.</div>
      <div className="scanner-hub-grid">
        {ANALYSERS.map((a) => {
          const I = Icon[a.icon];
          return (
            <button
              key={a.key}
              className="scanner-hub-card"
              style={{ "--icon-color": a.color }}
              onClick={() => onSelect(a.key)}
            >
              <div className="scanner-hub-icon">{I && <I />}</div>
              <div>
                <div className="scanner-hub-name">{a.label}</div>
                <div className="scanner-hub-desc">{a.desc}</div>
              </div>
              <div className="scanner-hub-cta">Open analyser →</div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
