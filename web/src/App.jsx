import React, { useCallback, useEffect, useRef, useState } from "react";
import { getToken, setToken } from "./api.js";
import { Icon } from "./icons.jsx";
import CommandPalette from "./CommandPalette.jsx";
import ErrorBoundary from "./ErrorBoundary.jsx";
import Landing from "./pages/Landing.jsx";
import Login from "./pages/Login.jsx";
import Home from "./pages/Home.jsx";
import Analytics from "./pages/Analytics.jsx";
import Watchlist from "./pages/Watchlist.jsx";
import Scanner from "./pages/Scanner.jsx";
import Audit from "./pages/Audit.jsx";
import Paper from "./pages/Paper.jsx";
import Profile from "./pages/Profile.jsx";
import Admin from "./pages/Admin.jsx";
import IPO from "./pages/IPO.jsx";
import PromoterTrades from "./pages/PromoterTrades.jsx";
import Deals from "./pages/Deals.jsx";
import MutualFunds from "./pages/MutualFunds.jsx";
import PromoterBuying from "./pages/PromoterBuying.jsx";
import Insights from "./pages/Insights.jsx";
import PublicShell from "./pages/PublicShell.jsx";

// Maps a real URL path to one of the no-login views, for shareable/indexable
// links (e.g. /promoter-trades/RELIANCE) — everything else in the app stays
// purely state-driven (see the view/localStorage machinery below), only
// these few paths are ever read from window.location.
const PUBLIC_PATHS = [
  { re: /^\/ipo\/?([^/]*)$/, view: "ipo" },
  { re: /^\/promoter-trades\/?([^/]*)$/, view: "promoter-trades" },
  { re: /^\/bulk-deals\/?([^/]*)$/, view: "bulk" },
  { re: /^\/block-deals\/?([^/]*)$/, view: "block" },
];

function matchPublicPath(pathname) {
  for (const { re, view } of PUBLIC_PATHS) {
    const m = pathname.match(re);
    if (m) return { view, symbol: m[1] ? decodeURIComponent(m[1]) : null };
  }
  return null;
}

// Flat top-level entries and collapsible groups. A group's `items` are the
// actual navigable leaves; the group itself is just a collapsible header.
const NAV = [
  { key: "home", label: "Home", icon: "home" },
  { key: "watchlist", label: "Watchlist", icon: "star" },
  { key: "analytics", label: "Analytics", icon: "chart" },
  { key: "insights", label: "Insights", icon: "pulse" },
  {
    group: "scanners", label: "Scanners", icon: "scan", items: [
      { key: "scanner:pine", label: "Pine Scanner", icon: "scan" },
      { key: "scanner:weekly", label: "Weekly Scanner", icon: "scan" },
      { key: "patterns:cup_handle", label: "Cup and Handle", icon: "scan" },
      { key: "patterns:downtrend_breakout", label: "Downtrend Breakout", icon: "scan" },
      { key: "patterns:rectangle", label: "Rectangle Box", icon: "scan" },
    ],
  },
  {
    // Raw feeds — each already has its own bottom-nav tab on mobile, so this
    // group stays sidebar-only there rather than duplicating that access.
    group: "markets", label: "Markets", icon: "trending", mobileHidden: true, items: [
      { key: "ipo", label: "IPO Tracker", icon: "rocket" },
      { key: "promoter", label: "Promoter Trades", icon: "pulse" },
      { key: "bulk", label: "Bulk Deals", icon: "list" },
      { key: "block", label: "Block Deals", icon: "list" },
    ],
  },
  {
    // Derived/aggregated views — kept out of Markets on purpose (raw feed vs.
    // analysis are different mental models) and shown on mobile since
    // neither has a bottom-nav tab of its own.
    group: "analyser", label: "Analyser", icon: "pulse", items: [
      { key: "mutual-funds", label: "Mutual-Funds", icon: "wallet" },
      { key: "promoter-buying", label: "Promoter Analyser", icon: "trending" },
    ],
  },

  { key: "audit", label: "Audit", icon: "list" },
  { key: "paper", label: "Paper Trading", icon: "wallet" },
  { key: "admin", label: "Admin", icon: "shield", admin: true },
];

const TITLES = {
  home: "Trending",
  watchlist: "Watchlist",
  analytics: "Analytics Dashboard",
  insights: "Insights",
  "scanner:pine": "Pine Scanner",
  "scanner:weekly": "Weekly Scanner",
  "patterns:cup_handle": "Cup and Handle",
  "patterns:downtrend_breakout": "Downtrend Breakout",
  "patterns:rectangle": "Rectangle Box",
  ipo: "IPO Tracker",
  promoter: "Promoter Trades",
  "promoter-buying": "Promoter Buying Analyser",
  bulk: "Bulk Deals",
  block: "Block Deals",
  "mutual-funds": "Mutual Fund Analyser",
  audit: "Signal Audit",
  paper: "Paper Trading",
  profile: "Profile",
  admin: "Admin — Candle Tools",
};

// Groups a view key belongs to, e.g. "scanner:pine" → "scanners".
const GROUP_OF_KEY = NAV.filter((n) => n.items).reduce((acc, g) => {
  g.items.forEach((it) => { acc[it.key] = g.group; });
  return acc;
}, {});

// The ordered list of tabs driven by bottom-nav swipe gestures.
const SWIPE_TABS = ["home", "ipo", "promoter", "bulk", "block"];

// Returns true when every scrollable ancestor is at its leftmost position
// (nothing left to pan through, so a right-swipe can open the sidebar or go prev-tab).
function canSwipeLeft(el) {
  let node = el;
  while (node && node !== document.body) {
    const ox = window.getComputedStyle(node).overflowX;
    if ((ox === "auto" || ox === "scroll") && node.scrollWidth > node.clientWidth) {
      if (node.scrollLeft > 4) return false;
    }
    node = node.parentElement;
  }
  return true;
}

// Returns true when every scrollable ancestor is at its rightmost position
// (nothing right to pan through, so a left-swipe can advance to the next tab).
function canSwipeRight(el) {
  let node = el;
  while (node && node !== document.body) {
    const ox = window.getComputedStyle(node).overflowX;
    if ((ox === "auto" || ox === "scroll") && node.scrollWidth > node.clientWidth) {
      if (node.scrollLeft < node.scrollWidth - node.clientWidth - 4) return false;
    }
    node = node.parentElement;
  }
  return true;
}

export default function App() {
  // Computed once at mount from the real URL — the one place this app reads
  // window.location.pathname, since everywhere else navigation is pure state
  // (see `view` below). Only matters for a visitor who isn't logged in yet;
  // an already-authed user just gets their normal last-viewed page.
  const [publicRoute] = useState(() => matchPublicPath(window.location.pathname));
  const [theme, setTheme] = useState(localStorage.getItem("theme") || "dark");
  const [view, setView] = useState(localStorage.getItem("view") || "home");
  const [slideDir, setSlideDir] = useState("fade");
  const [menuOpen, setMenuOpen] = useState(false);
  const swipeRef = useRef({}); // { startX, startY, target }
  const mainRef = useRef(null);
  const [ptrState, setPtrState] = useState("idle"); // idle | pulling | active | refreshing
  const [ptrHeight, setPtrHeight] = useState(0);
  const ptrRef = useRef({ startY: 0, pulling: false });
  const [collapsedGroups, setCollapsedGroups] = useState(() => {
    try { return JSON.parse(localStorage.getItem("navCollapsedGroups") || "{}"); } catch { return {}; }
  });
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [sidebarPinned, setSidebarPinned] = useState(() => {
    const saved = localStorage.getItem("sidebarPinned");
    return saved === null ? true : saved === "true";
  });
  const [showLogin, setShowLogin] = useState(false); // false → marketing landing
  const [user, setUser] = useState(() => {
    try { return JSON.parse(localStorage.getItem("user") || "null"); } catch { return null; }
  });
  const authed = !!getToken() && !!user;
  const isAdmin = !!(user && user.is_admin);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("theme", theme);
  }, [theme]);

  // Remember the active page so a refresh reopens where you were.
  useEffect(() => {
    localStorage.setItem("view", view);
  }, [view]);

  // If navigation lands on an item inside a collapsed group, expand it so
  // the active item is never hidden.
  useEffect(() => {
    const group = GROUP_OF_KEY[view];
    if (group) setCollapsedGroups((prev) => (prev[group] ? { ...prev, [group]: false } : prev));
  }, [view]);

  useEffect(() => {
    localStorage.setItem("navCollapsedGroups", JSON.stringify(collapsedGroups));
  }, [collapsedGroups]);

  function toggleGroup(key) {
    if (!sidebarPinned) {
      setSidebarPinned(true);
      localStorage.setItem("sidebarPinned", "true");
      setCollapsedGroups((prev) => ({ ...prev, [key]: false }));
    } else {
      setCollapsedGroups((prev) => ({ ...prev, [key]: !prev[key] }));
    }
  }

  function toggleSidebarPin() {
    setSidebarPinned((prev) => {
      const next = !prev;
      localStorage.setItem("sidebarPinned", String(next));
      return next;
    });
  }

  useEffect(() => {
    const onExpire = () => { setUser(null); localStorage.removeItem("user"); };
    window.addEventListener("auth-expired", onExpire);
    return () => window.removeEventListener("auth-expired", onExpire);
  }, []);

  // Handle browser back button (both for login and internal app views)
  useEffect(() => {
    // Set the initial state so the first back navigation works properly
    window.history.replaceState({ view }, "");

    const onPopState = (e) => {
      if (e.state && e.state.view) {
        setSlideDir("fade"); // Default to fade on browser back/forward
        setView(e.state.view);
      }
      setShowLogin(false); // Always close login screen if it was open
      setPaletteOpen(false); // ALWAYS close the command palette if it was open
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  function openPalette() {
    setPaletteOpen(true);
    window.history.pushState(Object.assign({}, window.history.state, { modal: "cmdk" }), "");
  }

  // Cmd/Ctrl-K toggles the command palette.
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        if (paletteOpen) {
          window.history.back();
        } else {
          openPalette();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [paletteOpen]);

  // ── Custom pull-to-refresh (replaces native since body is overflow:hidden) ──
  const PTR_THRESHOLD = 60;
  useEffect(() => {
    const el = mainRef.current;
    if (!el) return;

    const onTouchStart = (e) => {
      // Only start tracking if scrolled to the very top
      if (el.scrollTop > 0) return;
      ptrRef.current = { startY: e.touches[0].clientY, pulling: false };
    };

    const onTouchMove = (e) => {
      const { startY } = ptrRef.current;
      if (startY === 0) return;
      const dy = e.touches[0].clientY - startY;
      if (dy < 0) { ptrRef.current.startY = 0; return; } // pulling up, ignore

      // Don't interfere until we're sure user is pulling down at the top
      if (el.scrollTop > 0) { ptrRef.current.startY = 0; return; }

      ptrRef.current.pulling = true;
      // Apply resistance (diminishing returns past threshold)
      const pull = Math.min(dy * 0.45, 100);
      setPtrHeight(pull);
      setPtrState(pull >= PTR_THRESHOLD ? "active" : "pulling");

      // Prevent the page bounce on iOS
      if (dy > 0 && el.scrollTop <= 0) {
        e.preventDefault();
      }
    };

    const onTouchEnd = () => {
      if (!ptrRef.current.pulling) { ptrRef.current = { startY: 0, pulling: false }; return; }

      if (ptrHeight >= PTR_THRESHOLD) {
        setPtrState("refreshing");
        setPtrHeight(0);
        // Reload after a brief visual delay
        setTimeout(() => window.location.reload(), 600);
      } else {
        setPtrState("idle");
        setPtrHeight(0);
      }
      ptrRef.current = { startY: 0, pulling: false };
    };

    el.addEventListener("touchstart", onTouchStart, { passive: true });
    el.addEventListener("touchmove", onTouchMove, { passive: false });
    el.addEventListener("touchend", onTouchEnd, { passive: true });
    return () => {
      el.removeEventListener("touchstart", onTouchStart);
      el.removeEventListener("touchmove", onTouchMove);
      el.removeEventListener("touchend", onTouchEnd);
    };
  }, [ptrHeight]);

  function onAuthed(u) {
    setUser(u);
    localStorage.setItem("user", JSON.stringify(u));
    setView("home");
  }
  function logout() {
    setToken("");
    setUser(null);
    localStorage.removeItem("user");
  }

  if (!authed) {
    if (publicRoute && !showLogin) {
      return (
        <PublicShell
          view={publicRoute.view}
          symbol={publicRoute.symbol}
          theme={theme}
          onToggleTheme={() => setTheme(theme === "dark" ? "light" : "dark")}
          onGetStarted={() => {
            window.history.pushState({ page: "login" }, "");
            setShowLogin(true);
          }}
        />
      );
    }
    return showLogin ? (
      <Login onAuthed={onAuthed} onBack={() => window.history.back()} />
    ) : (
      <Landing
        theme={theme}
        onToggleTheme={() => setTheme(theme === "dark" ? "light" : "dark")}
        onGetStarted={() => {
          window.history.pushState({ page: "login" }, "");
          setShowLogin(true);
        }}
      />
    );
  }

  const userId = user.id;
  function render() {
    const p = { userId };
    switch (view) {
      case "home": return <Home {...p} />;
      case "watchlist": return <Watchlist {...p} />;
      case "analytics": return <Analytics {...p} />;
      case "insights": return <Insights isAdmin={isAdmin} />;
      case "scanner:pine": return <Scanner source="pine" {...p} />;
      case "scanner:weekly": return <Scanner source="weekly" {...p} />;
      case "patterns:cup_handle": return <Scanner source="patterns" pattern="pattern_cup_handle" {...p} />;
      case "patterns:downtrend_breakout": return <Scanner source="patterns" pattern="pattern_downtrend_breakout" {...p} />;
      case "patterns:rectangle": return <Scanner source="patterns" pattern="pattern_rectangle" {...p} />;
      case "ipo": return <IPO isAdmin={isAdmin} />;
      case "promoter": return <PromoterTrades isAdmin={isAdmin} />;
      case "promoter-buying": return <PromoterBuying />;
      case "bulk": return <Deals type="bulk" isAdmin={isAdmin} />;
      case "block": return <Deals type="block" isAdmin={isAdmin} />;
      case "mutual-funds": return <MutualFunds />;
      case "audit": return <Audit isAdmin={isAdmin} />;
      case "paper": return <Paper {...p} />;
      case "profile": return <Profile {...p} onLogout={logout} />;
      case "admin": return isAdmin ? <Admin /> : <Home {...p} />;
      default: return null;
    }
  }

  const initial = (user.email || "?").slice(0, 1).toUpperCase();
  function go(key) {
    if (key !== view) {
      const fromIdx = SWIPE_TABS.indexOf(view);
      const toIdx = SWIPE_TABS.indexOf(key);
      if (fromIdx !== -1 && toIdx !== -1) {
        setSlideDir(toIdx > fromIdx ? "left" : "right");
      } else {
        setSlideDir("fade");
      }

      // Always use replaceState for top-level navigation so we don't build up
      // a massive back-button history of tab switches.
      window.history.replaceState({ view: key }, "");
      setView(key);
    }
    setMenuOpen(false);
    setPaletteOpen(false);
  }

  const nav = NAV
    .filter((n) => !n.admin || isAdmin)
    .map((n) => (n.items ? { ...n, items: n.items.filter((it) => !it.admin || isAdmin) } : n));
  const leafItems = nav.flatMap((n) => n.items || [n]);
  const commands = [
    ...leafItems.map((n) => ({ id: "nav-" + n.key, label: n.label, hint: "Go to page", run: () => go(n.key) })),
    { id: "theme", label: "Toggle theme (dark / light)", hint: "Appearance", run: () => { setTheme(theme === "dark" ? "light" : "dark"); window.history.back(); } },
    { id: "signout", label: "Sign out", hint: "Session", run: () => { logout(); window.history.back(); } },
  ];

  return (
    <div className="app">
      {menuOpen && <div className="backdrop" onClick={() => setMenuOpen(false)} />}
      <aside
        className={"sidebar" + (menuOpen ? " open" : "") + (sidebarPinned ? " pinned" : " pinned-collapsed")}
        onTouchStart={(e) => {
          const t = e.touches[0];
          swipeRef.current = { startX: t.clientX, startY: t.clientY, target: e.target };
        }}
        onTouchEnd={(e) => {
          const { startX, startY } = swipeRef.current;
          if (startX == null) return;
          const t = e.changedTouches[0];
          const dx = t.clientX - startX;
          const dy = t.clientY - startY;
          swipeRef.current = {};
          if (Math.abs(dx) < Math.abs(dy)) return; // more vertical than horizontal
          if (Math.abs(dx) < 50) return;            // too short
          if (dx < 0 && menuOpen) setMenuOpen(false); // swipe left → close
        }}
      >
        <div className="brand">
          <div className="brand-logo" onClick={() => go("home")} title="Go to home">
            <span className="prompt">&gt;_</span>
            <span className="brand-text">Trade<em>Nexus</em></span>
          </div>
          <button className="sidebar-collapse-btn" onClick={toggleSidebarPin} title={sidebarPinned ? "Collapse sidebar" : "Expand sidebar"}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              {sidebarPinned
                ? <path d="M15 18l-6-6 6-6" />
                : <path d="M9 18l6-6-6-6" />
              }
            </svg>
          </button>
        </div>
        {nav.map((n) => {
          if (n.items) {
            const GroupIcon = Icon[n.icon];
            const collapsed = !!collapsedGroups[n.group];
            const hasActive = n.items.some((it) => it.key === view);
            return (
              <div className={"nav-group" + (n.mobileHidden ? " hide-on-mobile" : "")} key={n.group}>
                <div
                  className={"nav-group-head" + (hasActive ? " active" : "")}
                  onClick={() => toggleGroup(n.group)}
                  title={n.label}
                >
                  {GroupIcon && <GroupIcon />}<span>{n.label}</span>
                  <Icon.chevron className={"nav-chevron" + (collapsed ? " collapsed" : "")} />
                </div>
                {!collapsed && n.items.map((it) => {
                  const I = Icon[it.icon];
                  return (
                    <div
                      key={it.key}
                      className={"nav-item nav-sub" + (view === it.key ? " active" : "")}
                      onClick={() => go(it.key)}
                      title={it.label}
                    >
                      {I && <I />}<span>{it.label}</span>
                    </div>
                  );
                })}
              </div>
            );
          }
          const I = Icon[n.icon];
          return (
            <div
              key={n.key}
              className={"nav-item" + (view === n.key ? " active" : "") + (n.mobileHidden ? " hide-on-mobile" : "")}
              onClick={() => go(n.key)}
              title={n.label}
            >
              {I && <I />}<span>{n.label}</span>
            </div>
          );
        })}

      </aside>

      <div
        className="main"
        ref={mainRef}
        onTouchStart={(e) => {
          const t = e.touches[0];
          swipeRef.current = { startX: t.clientX, startY: t.clientY, target: e.target, isLocked: false, isVertical: false };
        }}
        onTouchMove={(e) => {
          const state = swipeRef.current;
          if (state.startX == null || state.isLocked) return;
          const t = e.changedTouches[0];
          const dx = t.clientX - state.startX;
          const dy = t.clientY - state.startY;
          if (Math.abs(dx) > 10 || Math.abs(dy) > 10) {
            state.isLocked = true;
            state.isVertical = Math.abs(dy) > Math.abs(dx);
          }
        }}
        onTouchEnd={(e) => {
          const { startX, target, isVertical } = swipeRef.current;
          if (startX == null) return;
          const t = e.changedTouches[0];
          const dx = t.clientX - startX;
          swipeRef.current = {};

          // Ignore swipes that started in a portaled modal (they bubble in React but aren't in .main DOM)
          if (target && !target.closest('.main')) return;

          if (isVertical) return;                   // Locked to vertical scroll — ignore
          if (Math.abs(dx) < 50) return;            // too short — ignore

          const tabIdx = SWIPE_TABS.indexOf(view);

          if (dx < 0) {
            // ── Swipe LEFT (finger moves right-to-left) ──
            // Close sidebar if open, otherwise advance to next tab.
            if (menuOpen) { setMenuOpen(false); return; }
            if (!canSwipeRight(target)) return; // content still has room to scroll right
            const next = tabIdx !== -1 && tabIdx < SWIPE_TABS.length - 1
              ? SWIPE_TABS[tabIdx + 1] : null;
            if (next) go(next);
          } else {
            // ── Swipe RIGHT (finger moves left-to-right) ──
            // Go to previous tab; if already on the first tab open the sidebar.
            if (!canSwipeLeft(target)) return; // content still has room to scroll left
            if (tabIdx > 0) {
              go(SWIPE_TABS[tabIdx - 1]);
            } else if (!menuOpen) {
              setMenuOpen(true);
            }
          }
        }}
      >
        <div className="topbar">
          <div className="row" style={{ gap: 12 }}>
            <button className="icon-btn hamburger" onClick={() => setMenuOpen(true)} aria-label="Menu"><Icon.menu /></button>
            <h1>{TITLES[view]}</h1>
          </div>
          <div className="topbar-right">
            <button className="cmdk-trigger" title="Command palette" onClick={openPalette}>
              <Icon.search />
              <span>Search</span>
              <kbd>⌘K</kbd>
            </button>
            <button
              className="icon-btn"
              title="Toggle theme"
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            >
              {theme === "dark" ? <Icon.sun /> : <Icon.moon />}
            </button>
            <button
              className="avatar-btn"
              title="Profile"
              onClick={() => go("profile")}
              style={{
                width: 32, height: 32, borderRadius: '50%',
                background: 'var(--accent)', color: 'var(--bg)',
                display: 'grid', placeItems: 'center',
                fontWeight: 700, fontSize: 13, border: 'none',
                cursor: 'pointer', padding: 0
              }}
            >
              {initial}
            </button>
          </div>
        </div>
        <div
          className={`ptr-indicator${ptrState === "pulling" ? " ptr-pulling" : ""}${ptrState === "active" ? " ptr-active" : ""}${ptrState === "refreshing" ? " ptr-refreshing ptr-active" : ""}${ptrState === "idle" && ptrHeight === 0 ? " ptr-snapping" : ""}`}
          style={{ height: ptrState === "refreshing" ? 48 : ptrHeight }}
        >
          <div className="ptr-spinner" />
        </div>
        <div className={`content slide-${slideDir}`} key={view}><ErrorBoundary>{render()}</ErrorBoundary></div>
      </div>

      <CommandPalette open={paletteOpen} onClose={() => window.history.back()} commands={commands} />

      {/* ── Groww-style bottom nav (mobile only) ── */}
      <nav className="bottom-nav" aria-label="Quick navigation">
        {[
          { key: "home", label: "Dashboard", icon: <Icon.home /> },
          { key: "ipo", label: "IPO", icon: <Icon.rocket /> },
          { key: "promoter", label: "Promoter", icon: <Icon.pulse /> },
          { key: "bulk", label: "Bulk Deals", icon: <Icon.list /> },
          { key: "block", label: "Block Deals", icon: <Icon.list /> },
        ].map((tab) => (
          <button
            key={tab.key}
            className={"bn-tab" + (view === tab.key ? " bn-active" : "")}
            onClick={() => go(tab.key)}
            aria-label={tab.label}
          >
            <span className="bn-icon">{tab.icon}</span>
            <span className="bn-label">{tab.label}</span>
          </button>
        ))}
      </nav>
    </div>
  );
}
