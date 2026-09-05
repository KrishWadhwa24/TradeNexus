import React from "react";
import { StatisticsSection } from "./optionsShared.jsx";

// Own sidebar page (was the "Statistics" tab) — date-range P&L, heatmap,
// closed algo trade list. StatisticsSection fetches its own data now that
// it's standalone.
export default function OptionStatistics({ userId }) {
  if (!userId) return <div className="empty">Select a user to view statistics.</div>;
  return <StatisticsSection userId={userId} />;
}
