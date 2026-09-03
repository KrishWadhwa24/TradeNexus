import React from "react";

// A single skeleton card that mimics the shape of a promoter/deal/IPO card.
export function SkeletonCard({ lines = 4 }) {
  return (
    <div className="skeleton-card">
      <div className="skeleton-title" />
      <div className="skeleton-row">
        <div className="skeleton-chip" />
        <div className="skeleton-chip" />
      </div>
      <div className="skeleton-block" />
      {Array.from({ length: lines }).map((_, i) => (
        <div key={i} className={"skeleton-line " + (i % 2 === 0 ? "w80" : "w60")} />
      ))}
    </div>
  );
}

// A grid of skeleton cards. `count` determines how many to show.
export function SkeletonGrid({ count = 6, lines = 4 }) {
  return (
    <div className="skeleton-grid skeleton">
      {Array.from({ length: count }).map((_, i) => (
        <SkeletonCard key={i} lines={lines} />
      ))}
    </div>
  );
}

// Signal performance skeleton — narrower with horizon bars.
export function SkeletonPerfGrid({ count = 4 }) {
  return (
    <div className="ins-perf-grid skeleton">
      {Array.from({ length: count }).map((_, i) => (
        <div className="skeleton-card" key={i}>
          <div className="skeleton-title" />
          <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 10 }}>
            {Array.from({ length: 4 }).map((_, j) => (
              <div key={j} style={{ display: "flex", flexDirection: "column", gap: 5 }}>
                <div className="skeleton-line w40" />
                <div className="skeleton-line w80" style={{ height: 20 }} />
                <div className="skeleton-line" style={{ height: 5 }} />
                <div className="skeleton-line w50" />
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
