import React from "react";
import { ChainBrowser } from "./optionsShared.jsx";

// Own sidebar page (was the "Option Chain" tab). No sibling state to
// refresh anymore now that it's standalone — buying just reloads the chain
// itself (ChainBrowser already does that); the new position shows up next
// time "My Option Trades" is visited.
export default function OptionChain({ userId }) {
  if (!userId) return <div className="empty">Select a user to browse the option chain.</div>;
  return <ChainBrowser userId={userId} />;
}
