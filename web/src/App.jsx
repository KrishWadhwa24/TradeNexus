import React, { useEffect, useState } from "react";
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
import Insights from "./pages/Insights.jsx";

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
    group: "markets", label: "Markets", icon: "trending", items: [
      { key: "ipo", label: "IPO Tracker", icon: "rocket" },
      { key: "promoter", label: "Promoter Trades", icon: "pulse" },
      { key: "bulk", label: "Bulk Deals", icon: "list" },
      { key: "block", label: "Block Deals", icon: "list" },
    ],
  },
  { key: "audit", label: "Audit", icon: "list" },
  { key: "paper", label: "Paper Trading", icon: "wallet" },
  { key: "profile", label: "Profile", icon: "user" },
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
  bulk: "Bulk Deals",
  block: "Block Deals",
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

export default function App() {
  const [theme, setTheme] = useState(localStorage.getItem("theme") || "dark");
  const [view, setView] = useState(localStorage.getItem("view") || "home");
  const [menuOpen, setMenuOpen] = useState(false);
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

  // Give the login screen a real history entry so the browser Back button
  // returns to the landing page instead of leaving the app entirely.
  useEffect(() => {
    const onPopState = () => setShowLogin(false);
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  // Cmd/Ctrl-K toggles the command palette.
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

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
    return showLogin ? (
      <Login onAuthed={onAuthed} onBack={() => window.history.back()} />
    ) : (
      <Landing
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
      case "bulk": return <Deals type="bulk" isAdmin={isAdmin} />;
      case "block": return <Deals type="block" isAdmin={isAdmin} />;
      case "audit": return <Audit isAdmin={isAdmin} />;
      case "paper": return <Paper {...p} />;
      case "profile": return <Profile {...p} onLogout={logout} />;
      case "admin": return isAdmin ? <Admin /> : <Home {...p} />;
      default: return null;
    }
  }

  const initial = (user.email || "?").slice(0, 1).toUpperCase();
  function go(key) { setView(key); setMenuOpen(false); }

  const nav = NAV
    .filter((n) => !n.admin || isAdmin)
    .map((n) => (n.items ? { ...n, items: n.items.filter((it) => !it.admin || isAdmin) } : n));
  const leafItems = nav.flatMap((n) => n.items || [n]);
  const commands = [
    ...leafItems.map((n) => ({ id: "nav-" + n.key, label: n.label, hint: "Go to page", run: () => go(n.key) })),
    { id: "theme", label: "Toggle theme (dark / light)", hint: "Appearance", run: () => setTheme(theme === "dark" ? "light" : "dark") },
    { id: "signout", label: "Sign out", hint: "Session", run: logout },
  ];

  return (
    <div className="app">
      {menuOpen && <div className="backdrop" onClick={() => setMenuOpen(false)} />}
      <aside className={"sidebar" + (menuOpen ? " open" : "") + (sidebarPinned ? " pinned" : " pinned-collapsed")}>
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
              <div className="nav-group" key={n.group}>
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
              className={"nav-item" + (view === n.key ? " active" : "")}
              onClick={() => go(n.key)}
              title={n.label}
            >
              {I && <I />}<span>{n.label}</span>
            </div>
          );
        })}
        <div className="sidebar-foot">
          <div className="user-chip">
            <span className="avatar">{initial}</span>
            <div className="user-info" style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis" }}>{user.email}</div>
            </div>
          </div>
        </div>
      </aside>

      <div className="main">
        <div className="topbar">
          <div className="row" style={{ gap: 12 }}>
            <button className="icon-btn hamburger" onClick={() => setMenuOpen(true)} aria-label="Menu"><Icon.menu /></button>
            <h1>{TITLES[view]}</h1>
          </div>
          <div className="topbar-right">
            <button className="cmdk-trigger" title="Command palette" onClick={() => setPaletteOpen(true)}>
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
          </div>
        </div>
        <div className="content" key={view}><ErrorBoundary>{render()}</ErrorBoundary></div>
      </div>

      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} commands={commands} />
    </div>
  );
}
