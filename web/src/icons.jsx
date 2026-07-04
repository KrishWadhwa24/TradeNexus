import React from "react";

// Minimal stroke icon set (inherits currentColor via CSS `stroke`).
const s = { fill: "none", stroke: "currentColor", strokeWidth: 1.8, strokeLinecap: "round", strokeLinejoin: "round" };

export const Icon = {
  home: () => (<svg viewBox="0 0 24 24" {...s}><path d="M3 10.5 12 3l9 7.5" /><path d="M5 9.5V21h14V9.5" /></svg>),
  chart: () => (<svg viewBox="0 0 24 24" {...s}><path d="M4 20V4" /><path d="M4 20h16" /><path d="M7 16l3-4 3 2 4-6" /></svg>),
  scan: () => (<svg viewBox="0 0 24 24" {...s}><path d="M4 8V5a1 1 0 0 1 1-1h3" /><path d="M20 8V5a1 1 0 0 0-1-1h-3" /><path d="M4 16v3a1 1 0 0 0 1 1h3" /><path d="M20 16v3a1 1 0 0 1-1 1h-3" /><path d="M4 12h16" /></svg>),
  list: () => (<svg viewBox="0 0 24 24" {...s}><path d="M8 6h12M8 12h12M8 18h12" /><circle cx="4" cy="6" r="1" /><circle cx="4" cy="12" r="1" /><circle cx="4" cy="18" r="1" /></svg>),
  star: () => (<svg viewBox="0 0 24 24" {...s}><path d="M12 3l2.7 5.5 6 .9-4.3 4.2 1 6-5.4-2.8L6.6 19.6l1-6L3.3 9.4l6-.9L12 3Z" /></svg>),
  wallet: () => (<svg viewBox="0 0 24 24" {...s}><rect x="3" y="6" width="18" height="13" rx="2.5" /><path d="M3 10h18" /><circle cx="17" cy="14" r="1" /></svg>),
  user: () => (<svg viewBox="0 0 24 24" {...s}><circle cx="12" cy="8" r="4" /><path d="M4 21c0-4 3.5-6 8-6s8 2 8 6" /></svg>),
  menu: () => (<svg viewBox="0 0 24 24" {...s}><path d="M4 6h16M4 12h16M4 18h16" /></svg>),
  close: () => (<svg viewBox="0 0 24 24" {...s}><path d="M6 6l12 12M18 6L6 18" /></svg>),
  sun: () => (<svg viewBox="0 0 24 24" {...s}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19" /></svg>),
  moon: () => (<svg viewBox="0 0 24 24" {...s}><path d="M21 12.8A8.5 8.5 0 1 1 11.2 3a6.5 6.5 0 0 0 9.8 9.8Z" /></svg>),
};

// Brand mark inside the logo tile.
export function LogoMark() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M4 15l4-5 3 3 5-7" />
      <path d="M15 6h3v3" />
    </svg>
  );
}

// Decorative line-chart illustration for the Home hero.
export function HeroChart() {
  return (
    <svg className="hero-svg" viewBox="0 0 260 180" fill="none">
      <defs>
        <linearGradient id="hg" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#7c3aed" stopOpacity="0.35" />
          <stop offset="1" stopColor="#7c3aed" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d="M10 150 L60 120 L100 135 L150 70 L200 90 L250 25" stroke="#6d28d9" strokeWidth="3" fill="none" strokeLinecap="round" />
      <path d="M10 150 L60 120 L100 135 L150 70 L200 90 L250 25 L250 175 L10 175 Z" fill="url(#hg)" />
      <circle cx="250" cy="25" r="5" fill="#6d28d9" />
    </svg>
  );
}

// Empty-state illustration.
export function EmptyArt() {
  return (
    <svg viewBox="0 0 120 120" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <rect x="18" y="24" width="84" height="72" rx="8" opacity="0.5" />
      <path d="M30 78l16-18 12 10 20-26" opacity="0.9" />
      <path d="M78 44h10v10" opacity="0.9" />
    </svg>
  );
}
