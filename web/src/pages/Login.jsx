import React, { useState } from "react";
import { authApi, setToken } from "../api.js";
import { Box, Button, TextField, Typography, Grid, Link, CircularProgress } from '@mui/material';

export default function Login({ onAuthed }) {
  const [mode, setMode] = useState("login"); // login | register
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const fn = mode === "login" ? authApi.login : authApi.register;
      const r = await fn(email, password);
      setToken(r.token);
      onAuthed(r.user);
    } catch (e2) {
      setErr(e2.message);
    } finally {
      setBusy(false);
    }
  }

  const Art = () => (
    <Grid item xs={12} md={6} sx={{
        display: { xs: 'none', md: 'flex' },
        flexDirection: 'column',
        justifyContent: 'center',
        gap: 2,
        p: 7,
        background: (theme) => `
            radial-gradient(120% 100% at 0% 0%, ${theme.palette.mode === 'dark' ? 'rgba(109,110,252,.18)' : 'rgba(91,83,240,.1)'}, transparent 55%),
            radial-gradient(100% 100% at 100% 100%, ${theme.palette.mode === 'dark' ? 'rgba(62,207,142,.10)' : 'rgba(18,165,106,.1)'}, transparent 55%),
            ${theme.palette.background.paper}`,
        borderRight: (theme) => `1px solid ${theme.palette.divider}`,
      }}>
        <Typography variant="h6" noWrap component="div" sx={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 20, letterSpacing: '-.5px' }}>
          <span style={{ color: 'var(--green)' }}>&gt;_</span>
          Trade<em style={{ fontStyle: 'normal', color: 'var(--accent-2)' }}>Nexus</em>
        </Typography>
      <Typography variant="overline" sx={{
          color: 'secondary.main',
          border: '1px solid',
          borderColor: 'secondary.main',
          backgroundColor: (theme) => theme.palette.mode === 'dark' ? 'rgba(62,207,142,.1)' : 'rgba(18,165,106,.1)',
          px: 1.5, py: .5, borderRadius: 99, alignSelf: 'flex-start'
      }}>
          // UNIFIED_STOCK_SCANNER
      </Typography>
      <Typography variant="h2" sx={{ fontWeight: 700, letterSpacing: -2, '& .accent': { color: 'primary.main' } }}>
        Find winning stocks<br />before the <span className="accent">crowd.</span>
      </Typography>
      <Typography variant="body1" color="text.secondary" sx={{ maxWidth: 400 }}>
        Pine &amp; weekly scanners over NSE + BSE data, live analytics, Telegram alerts, and paper trading — all in one terminal.
      </Typography>
    </Grid>
  );

  return (
    <Grid container component="main" sx={{ height: '100vh', backgroundImage: `
        linear-gradient(rgba(255,255,255,.022) 1px, transparent 1px),
        linear-gradient(90deg, rgba(255,255,255,.022) 1px, transparent 1px)`,
        backgroundSize: '44px 44px',
    }}>
      <Art />
      <Grid item xs={12} md={6} sx={{ display: 'grid', placeItems: 'center' }}>
        <Box
          component="form"
          onSubmit={submit}
          sx={{
            display: 'flex',
            flexDirection: 'column',
            gap: 2,
            p: {xs: 3, sm: 7},
            width: '100%',
            maxWidth: 460,
            mx: 'auto',
          }}
        >
          <Typography variant="h4" sx={{ fontWeight: 700 }}>
            {mode === "login" ? "Welcome back" : "Create your account"}
          </Typography>
          <Typography color="text.secondary">
            {mode === "login" ? "Sign in to your dashboard." : "Start scanning in seconds."}
          </Typography>

          <TextField
            label="Email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="you@example.com"
            required
            variant="outlined"
            margin="normal"
          />
          <TextField
            label="Password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="min 6 characters"
            required
            variant="outlined"
            margin="normal"
          />

          {err && <Typography color="error" sx={{ mt: 1 }}>{err}</Typography>}

          <Button type="submit" variant="contained" size="large" disabled={busy} sx={{ mt: 2, py: 1.5 }}>
            {busy ? <CircularProgress size={24} color="inherit"/> : (mode === "login" ? "Sign in" : "Create account")}
          </Button>

          <Typography sx={{ mt: 2, textAlign: 'center' }}>
            {mode === "login" ? "New here? " : "Already have an account? "}
            <Link component="button" variant="body1" type="button" onClick={() => { setMode(m => m === 'login' ? 'register' : 'login'); setErr(""); }}>
              {mode === "login" ? "Create an account" : "Sign in"}
            </Link>
          </Typography>
        </Box>
      </Grid>
    </Grid>
  );
}
