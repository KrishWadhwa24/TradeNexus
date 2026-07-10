import React, { useEffect, useState } from "react";
import { getToken, setToken } from "./api.js";
import { Icon } from "./icons.jsx";
import Login from "./pages/Login.jsx";
import Home from "./pages/Home.jsx";
import Analytics from "./pages/Analytics.jsx";
import Watchlist from "./pages/Watchlist.jsx";
import Scanner from "./pages/Scanner.jsx";
import Audit from "./pages/Audit.jsx";
import Paper from "./pages/Paper.jsx";
import Profile from "./pages/Profile.jsx";

const NAV = [
  { key: "home", label: "Home", icon: "home" },
  { key: "watchlist", label: "Watchlist", icon: "star" },
  { key: "analytics", label: "Analytics", icon: "chart" },
  { key: "scanner:pine", label: "Pine Scanner", icon: "scan", sub: true },
  { key: "scanner:weekly", label: "Weekly Scanner", icon: "scan", sub: true },
  { key: "patterns:cup_handle", label: "Cup and Handle", icon: "scan", sub: true },
  { key: "patterns:downtrend_breakout", label: "Downtrend Breakout", icon: "scan", sub: true },
  { key: "patterns:rectangle", label: "Rectangle Box", icon: "scan", sub: true },
  { key: "audit", label: "Audit", icon: "list" },
  { key: "paper", label: "Paper Trading", icon: "wallet" },
  { key: "profile", label: "Profile", icon: "user" },
];

const TITLES = {
  home: "Trending",
  watchlist: "Watchlist",
  analytics: "Analytics Dashboard",
  "scanner:pine": "Pine Scanner",
  "scanner:weekly": "Weekly Scanner",
  "patterns:cup_handle": "Cup and Handle",
  "patterns:downtrend_breakout": "Downtrend Breakout",
  "patterns:rectangle": "Rectangle Box",
  audit: "Signal Audit",
  paper: "Paper Trading",
  profile: "Profile",
};

export default function App() {
  const [theme, setTheme] = useState(localStorage.getItem("theme") || "dark");
  const [view, setView] = useState(localStorage.getItem("view") || "home");
  const [menuOpen, setMenuOpen] = useState(false);
  const [user, setUser] = useState(() => {
    try { return JSON.parse(localStorage.getItem("user") || "null"); } catch { return null; }
  });
  const authed = !!getToken() && !!user;

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("theme", theme);
  }, [theme]);

  // Remember the active page so a refresh reopens where you were.
  useEffect(() => {
    localStorage.setItem("view", view);
  }, [view]);

  useEffect(() => {
    const onExpire = () => { setUser(null); localStorage.removeItem("user"); };
    window.addEventListener("auth-expired", onExpire);
    return () => window.removeEventListener("auth-expired", onExpire);
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

  if (!authed) return <Login onAuthed={onAuthed} />;

  const userId = user.id;
  function render() {
    const p = { userId };
    switch (view) {
      case "home": return <Home {...p} />;
      case "watchlist": return <Watchlist {...p} />;
      case "analytics": return <Analytics {...p} />;
      case "scanner:pine": return <Scanner source="pine" {...p} />;
      case "scanner:weekly": return <Scanner source="weekly" {...p} />;
      case "patterns:cup_handle": return <Scanner source="patterns" pattern="pattern_cup_handle" {...p} />;
      case "patterns:downtrend_breakout": return <Scanner source="patterns" pattern="pattern_downtrend_breakout" {...p} />;
      case "patterns:rectangle": return <Scanner source="patterns" pattern="pattern_rectangle" {...p} />;
      case "audit": return <Audit />;
      case "paper": return <Paper {...p} />;
      case "profile": return <Profile {...p} />;
      default: return null;
    }
  }

  const initial = (user.email || "?").slice(0, 1).toUpperCase();
  function go(key) { setView(key); setMenuOpen(false); }

  return (
    <div className="app">
      {menuOpen && <div className="backdrop" onClick={() => setMenuOpen(false)} />}
      <aside className={"sidebar" + (menuOpen ? " open" : "")}>
        <div className="brand">
          <span className="prompt">&gt;_</span>
          Trade<em>Nexus</em>
        </div>
        {NAV.map((n) => {
          const I = Icon[n.icon];
          return (
            <div
              key={n.key}
              className={"nav-item" + (n.sub ? " nav-sub" : "") + (view === n.key ? " active" : "")}
              onClick={() => go(n.key)}
            >
              {I && <I />}<span>{n.label}</span>
            </div>
          );
        })}
        <div className="sidebar-foot">
          <div className="user-chip">
            <span className="avatar">{initial}</span>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis" }}>{user.email}</div>
              <a className="subtle" style={{ cursor: "pointer" }} onClick={logout}>Sign out</a>
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
            <button
              className="icon-btn"
              title="Toggle theme"
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            >
              {theme === "dark" ? <Icon.sun /> : <Icon.moon />}
            </button>
          </div>
        </div>
        <div className="content" key={view}>{render()}</div>
      </div>
    </div>
  );
}
