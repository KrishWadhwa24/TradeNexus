import React, { useEffect, useState, useMemo } from "react";
import { getToken, setToken } from "./api.js";

import Login from "./pages/Login.jsx";
import Home from "./pages/Home.jsx";
import Analytics from "./pages/Analytics.jsx";
import Watchlist from "./pages/Watchlist.jsx";
import Scanner from "./pages/Scanner.jsx";
import Audit from "./pages/Audit.jsx";
import PaperTrading from "./pages/PaperTrading.jsx";
import Profile from "./pages/Profile.jsx";

import { ThemeProvider, useTheme } from '@mui/material/styles';
import getTheme from './theme';
import CssBaseline from '@mui/material/CssBaseline';
import Box from '@mui/material/Box';
import Drawer from '@mui/material/Drawer';
import AppBar from '@mui/material/AppBar';
import Toolbar from '@mui/material/Toolbar';
import List from '@mui/material/List';
import Typography from '@mui/material/Typography';
import Divider from '@mui/material/Divider';
import IconButton from '@mui/material/IconButton';
import MenuIcon from '@mui/icons-material/Menu';
import ListItem from '@mui/material/ListItem';
import ListItemButton from '@mui/material/ListItemButton';
import ListItemIcon from '@mui/material/ListItemIcon';
import ListItemText from '@mui/material/ListItemText';
import Avatar from '@mui/material/Avatar';
import { Brightness4, Brightness7, Home as HomeIcon, Star, BarChart, Scanner as ScannerIcon, Assessment, AccountBalanceWallet, Person } from '@mui/icons-material';

const NAV = [
  { key: "home", label: "Home", icon: <HomeIcon /> },
  { key: "watchlist", label: "Watchlist", icon: <Star /> },
  { key: "analytics", label: "Analytics", icon: <BarChart /> },
  { key: "scanner:pine", label: "Pine Scanner", icon: <ScannerIcon />, sub: true },
  { key: "scanner:weekly", label: "Weekly Scanner", icon: <ScannerIcon />, sub: true },
  { key: "audit", label: "Audit", icon: <Assessment /> },
  { key: "paper", label: "Paper Trading", icon: <AccountBalanceWallet /> },
  { key: "profile", label: "Profile", icon: <Person /> },
];

const TITLES = {
  home: "Trending",
  watchlist: "Watchlist",
  analytics: "Analytics Dashboard",
  "scanner:pine": "Pine Scanner",
  "scanner:weekly": "Weekly Scanner",
  audit: "Signal Audit",
  paper: "Paper Trading",
  profile: "Profile",
};

const drawerWidth = 250;

function AppContent() {
  const [view, setView] = useState("home");
  const [mobileOpen, setMobileOpen] = useState(false);
  const [user, setUser] = useState(() => {
    try { return JSON.parse(localStorage.getItem("user") || "null"); } catch { return null; }
  });
  const authed = !!getToken() && !!user;

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

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen);
  };

  const initial = (user.email || "?").slice(0, 1).toUpperCase();

  const drawer = (
    <div>
      <Toolbar>
        <Typography variant="h6" noWrap component="div" sx={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 20, letterSpacing: '-.5px' }}>
          <span style={{ color: 'var(--green)' }}>&gt;_</span>
          Trade<em style={{ fontStyle: 'normal', color: 'var(--accent-2)' }}>Nexus</em>
        </Typography>
      </Toolbar>
      <List sx={{ p: 1 }}>
        {NAV.map((n) => (
          <ListItem key={n.key} disablePadding>
            <ListItemButton
              selected={view === n.key}
              onClick={() => { setView(n.key); if (mobileOpen) setMobileOpen(false);}}
              sx={{ pl: n.sub ? 4 : 2 }}
            >
              <ListItemIcon sx={{minWidth: 40, color: 'inherit' }}>{n.icon}</ListItemIcon>
              <ListItemText primary={n.label} />
            </ListItemButton>
          </ListItem>
        ))}
      </List>
      <Box sx={{ flexGrow: 1 }} />
      <Divider />
      <Box sx={{ p: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
        <Avatar sx={{ bgcolor: 'primary.main' }}>{initial}</Avatar>
        <Box sx={{ minWidth: 0 }}>
          <Typography variant="body2" sx={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis" }}>{user.email}</Typography>
          <Typography variant="caption" color="text.secondary" sx={{ cursor: "pointer" }} onClick={logout}>Sign out</Typography>
        </Box>
      </Box>
    </div>
  );

  function renderContent() {
    const p = { userId: user.id };
    switch (view) {
      case "home": return <Home />;
      case "watchlist": return <Watchlist {...p} />;
      case "analytics": return <Analytics {...p} />;
      case "scanner:pine": return <Scanner source="pine" {...p} />;
      case "scanner:weekly": return <Scanner source="weekly" {...p} />;
      case "audit": return <Audit />;
      case "paper": return <PaperTrading {...p} />;
      case "profile": return <Profile {...p} />;
      default: return null;
    }
  }

  return (
    <Box sx={{ display: 'flex' }}>
      <CssBaseline />
      <AppBar
        position="fixed"
        sx={{
          width: { sm: `calc(100% - ${drawerWidth}px)` },
          ml: { sm: `${drawerWidth}px` },
        }}
      >
        <Toolbar>
          <IconButton
            color="inherit"
            aria-label="open drawer"
            edge="start"
            onClick={handleDrawerToggle}
            sx={{ mr: 2, display: { sm: 'none' } }}
          >
            <MenuIcon />
          </IconButton>
          <Typography variant="h6" noWrap component="h1">
            {TITLES[view]}
          </Typography>
          <Box sx={{flexGrow: 1}}/>
          <ThemeToggleButton/>
        </Toolbar>
      </AppBar>
      <Box
        component="nav"
        sx={{ width: { sm: drawerWidth }, flexShrink: { sm: 0 } }}
        aria-label="mailbox folders"
      >
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={handleDrawerToggle}
          ModalProps={{ keepMounted: true }} // Better open performance on mobile.
          sx={{
            display: { xs: 'block', sm: 'none' },
            '& .MuiDrawer-paper': { boxSizing: 'border-box', width: drawerWidth },
          }}
        >
          {drawer}
        </Drawer>
        <Drawer
          variant="permanent"
          sx={{
            display: { xs: 'none', sm: 'block' },
            '& .MuiDrawer-paper': { boxSizing: 'border-box', width: drawerWidth },
          }}
          open
        >
          {drawer}
        </Drawer>
      </Box>
      <Box
        component="main"
        sx={{ flexGrow: 1, p: 3, width: { sm: `calc(100% - ${drawerWidth}px)` } }}
      >
        <Toolbar />
        {renderContent()}
      </Box>
    </Box>
  );
}

const ColorModeContext = React.createContext({ toggleColorMode: () => {} });

function ThemeToggleButton() {
  const theme = useTheme();
  const colorMode = React.useContext(ColorModeContext);
  return (
    <IconButton sx={{ ml: 1 }} onClick={colorMode.toggleColorMode} color="inherit">
      {theme.palette.mode === 'dark' ? <Brightness7 /> : <Brightness4 />}
    </IconButton>
  );
}

export default function App() {
  const [mode, setMode] = useState(localStorage.getItem("theme") || 'dark');

  const colorMode = useMemo(
    () => ({
      toggleColorMode: () => {
        setMode((prevMode) => {
            const newMode = prevMode === 'light' ? 'dark' : 'light';
            localStorage.setItem("theme", newMode);
            return newMode;
        });
      },
    }),
    [],
  );

  const theme = useMemo(() => getTheme(mode), [mode]);

  return (
    <ColorModeContext.Provider value={colorMode}>
      <ThemeProvider theme={theme}>
        <AppContent />
      </ThemeProvider>
    </ColorModeContext.Provider>
  )
}
