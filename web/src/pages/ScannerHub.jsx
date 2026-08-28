import React from "react";
import { Icon } from "../icons.jsx";

const SCANNERS = [
  {
    key: "scanner:pine",
    label: "Pine Scanner",
    icon: "fire",
    color: "#d95926",
    desc: "Momentum breakout signals — Chase Momentum strategy across 1D, 1W and 1M timeframes.",
  },
  {
    key: "scanner:weekly",
    label: "Weekly Scanner",
    icon: "trending",
    color: "#3987e5",
    desc: "Weekly structural setups — 52-week high breakouts, EMA stacks, and continuation patterns.",
  },
  {
    key: "patterns:cup_handle",
    label: "Cup and Handle",
    icon: "chart",
    color: "#199e70",
    desc: "Classic cup-and-handle base breakout pattern detector.",
  },
  {
    key: "patterns:downtrend_breakout",
    label: "Downtrend Breakout",
    icon: "pulse",
    color: "#d55181",
    desc: "Stocks breaking out of an established downtrend.",
  },
  {
    key: "patterns:rectangle",
    label: "Rectangle Box",
    icon: "scan",
    color: "#6a3fd1",
    desc: "Range/rectangle consolidation breakouts.",
  },
];

// ScannerHub is the "choose a scanner" landing page — clicking Scanners in
// the nav lands here first instead of dropping straight into one scanner's
// results, so the five scanners get a proper picker instead of a crowded
// sidebar submenu.
export default function ScannerHub({ onSelect }) {
  return (
    <div>
      <div className="section-title" style={{ marginBottom: 4 }}>Choose a scanner</div>
      <div className="subtle" style={{ marginBottom: 20 }}>Pick a strategy to see its current signals.</div>
      <div className="scanner-hub-grid">
        {SCANNERS.map((s) => {
          const I = Icon[s.icon];
          return (
            <button
              key={s.key}
              className="scanner-hub-card"
              style={{ "--icon-color": s.color }}
              onClick={() => onSelect(s.key)}
            >
              <div className="scanner-hub-icon">{I && <I />}</div>
              <div>
                <div className="scanner-hub-name">{s.label}</div>
                <div className="scanner-hub-desc">{s.desc}</div>
              </div>
              <div className="scanner-hub-cta">Open scanner →</div>
            </button>
          );
        })}
      </div>
    </div>
  );
}
