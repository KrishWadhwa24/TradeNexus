import React, { useState } from "react";
import { Icon } from "../icons.jsx";

// Drop-in "Share" button for anywhere a shareCard.js card can be built.
// `share` is an async function returning shareCard.js's shareCard() result
// (e.g. `() => shareStockCard(row)` or `() => shareIpoCard(ipo)`) — this
// component knows nothing about stocks/IPOs/whatever comes next, it just
// reacts to the result shape. Tries the native share sheet first (covers
// WhatsApp/Telegram/Gmail/etc. on browsers that support sharing files);
// where that's unsupported, downloads the card image and opens a small
// menu of text-share links instead.
// `compact` shrinks the button to fit inline next to other tiny controls
// (a table row's "Chart" button, an IPO card's corner) — set it to false
// when the button stands alongside normal-sized buttons instead (e.g. a
// modal's "Download PDF").
export default function ShareButton({ share, className = "btn-sm btn-ghost", title = "Share this card", compact = true }) {
  const [busy, setBusy] = useState(false);
  const [fallback, setFallback] = useState(null); // { caption } | null

  async function handleClick(e) {
    e.stopPropagation();
    setBusy(true);
    try {
      const result = await share();
      if (result?.downloaded) setFallback({ caption: result.caption });
    } catch {
      // Card generation/share failed silently — nothing meaningful to show
      // the user beyond "try again"; avoid a noisy error for a share button.
    } finally {
      setBusy(false);
    }
  }

  const caption = fallback?.caption || "";
  const links = fallback && [
    ["WhatsApp", `https://wa.me/?text=${encodeURIComponent(caption)}`],
    ["Telegram", `https://t.me/share/url?url=&text=${encodeURIComponent(caption)}`],
    ["Gmail", `mailto:?subject=${encodeURIComponent(caption)}&body=${encodeURIComponent(caption)}`],
  ];

  return (
    <span className="share-btn-wrap" style={{ position: "relative", display: "inline-flex" }}>
      <button
        className={className}
        style={compact
          ? { padding: "2px 6px", fontSize: 11, display: "inline-flex", alignItems: "center", gap: 4 }
          : { display: "inline-flex", alignItems: "center", gap: 6 }}
        disabled={busy}
        onClick={handleClick}
        title={title}
      >
        <Icon.share />
        {busy ? "…" : "Share"}
      </button>

      {fallback && (
        <div className="share-fallback-menu" onClick={(e) => e.stopPropagation()}>
          <div className="share-fallback-note">Card image downloaded — attach it, then send:</div>
          {links.map(([label, href]) => (
            <a key={label} href={href} target="_blank" rel="noreferrer" onClick={() => setFallback(null)}>
              {label}
            </a>
          ))}
          <button className="btn-sm" onClick={() => setFallback(null)}>Close</button>
        </div>
      )}
    </span>
  );
}
