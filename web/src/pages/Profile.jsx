import React, { useCallback, useEffect, useState } from "react";
import { api, fmt } from "../api.js";
import { Box, Typography, Button, TextField, Grid, Card, CardContent, CircularProgress, Alert, FormControlLabel, Checkbox, Snackbar } from "@mui/material";

function Stat({ label, value, cls }) {
    return (
      <Card>
        <CardContent>
          <Typography color="text.secondary" gutterBottom>{label}</Typography>
          <Typography variant="h5" component="div" className={cls}>
            {value}
          </Typography>
        </CardContent>
      </Card>
    );
  }

export default function Profile({ userId }) {
  const [sum, setSum] = useState(null);
  const [capital, setCapital] = useState("");
  const [tg, setTg] = useState({ bot_token: "", chat_id: "", enabled: true });
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    api.get(`/v1/users/${userId}/paper/summary`)
      .then((s) => { setSum(s); setCapital(String(s.starting_capital || "")); })
      .catch((e) => setErr(e.message));
    api.get(`/v1/users/${userId}/telegram`)
      .then((c) => setTg({ bot_token: c.bot_token || "", chat_id: c.chat_id || "", enabled: c.enabled }))
      .catch(() => {});
  }, [userId]);

  useEffect(() => { load(); }, [load]);

  async function saveCapital() {
    try {
      await api.put(`/v1/users/${userId}/paper/capital`, { capital: parseFloat(capital) || 0 });
      setMsg("Capital updated."); load();
    } catch (e) { setMsg("Failed: " + e.message); }
  }

  async function saveTg() {
    try {
      await api.put(`/v1/users/${userId}/telegram`, tg);
      setMsg("Telegram config saved.");
    } catch (e) { setMsg("Failed: " + e.message); }
  }

  async function testTg() {
    setMsg("Sending test…");
    try {
      await api.post(`/v1/telegram/test`, { user_id: userId });
      setMsg("Test sent — check your Telegram.");
    } catch (e) { setMsg("Test failed: " + e.message); }
  }

  if (!userId) return <Alert severity="info">Sign in to view your profile.</Alert>;
  if (err) return <Alert severity="error">{err}</Alert>;
  if (!sum) return (
    <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
        <CircularProgress size={24}/>
        <Typography>Loading…</Typography>
    </Box>
  );

  return (
    <Box>
       <style>{`.pos { color: #4caf50; } .neg { color: #f44336; }`}</style>
      <Typography variant="h5" sx={{ mb: 2, fontWeight: 'bold' }}>Virtual capital</Typography>
      <Card sx={{ p: 2, mb: 3 }}>
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
          <TextField
            type="number"
            label="Capital"
            value={capital}
            onChange={(e) => setCapital(e.target.value)}
            placeholder="e.g. 100000"
            size="small"
          />
          <Button variant="contained" onClick={saveCapital}>Save</Button>
        </Box>
        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
          Setting capital resets available cash to capital minus the cost of open positions.
        </Typography>
      </Card>

      <Typography variant="h5" sx={{ mb: 2, fontWeight: 'bold' }}>Telegram alerts</Typography>
      <Card sx={{ p: 2, mb: 3 }}>
        <Grid container spacing={2}>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Bot token"
              value={tg.bot_token}
              onChange={(e) => setTg({ ...tg, bot_token: e.target.value })}
              placeholder="123456:ABC-DEF…"
              size="small"
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Chat ID"
              value={tg.chat_id}
              onChange={(e) => setTg({ ...tg, chat_id: e.target.value })}
              placeholder="99999 or -100…"
              size="small"
            />
          </Grid>
        </Grid>
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center', mt: 2 }}>
            <FormControlLabel
                control={<Checkbox checked={tg.enabled} onChange={(e) => setTg({ ...tg, enabled: e.target.checked })} />}
                label="Enabled"
            />
          <Button variant="contained" onClick={saveTg}>Save</Button>
          <Button variant="outlined" onClick={testTg}>Send test</Button>
        </Box>
        <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
          Tip: message your bot once (press Start) before testing, and for a channel add the bot as admin and use the <code>-100…</code> id.
        </Typography>
      </Card>

      <Typography variant="h5" sx={{ mb: 2, fontWeight: 'bold' }}>Profit & Loss</Typography>
      <Grid container spacing={2}>
        <Grid item xs={12} sm={6} md={3}><Stat label="Starting capital" value={fmt(sum.starting_capital)} /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Cash balance" value={fmt(sum.cash_balance)} /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Equity" value={fmt(sum.equity)} /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Total P&L" value={fmt(sum.total_pnl)} cls={sum.total_pnl >= 0 ? "pos" : "neg"} /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Realized P&L" value={fmt(sum.realized_pnl)} cls={sum.realized_pnl >= 0 ? "pos" : "neg"} /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Booked profit" value={fmt(sum.booked_profit)} cls="pos" /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Booked loss" value={fmt(sum.booked_loss)} cls="neg" /></Grid>
        <Grid item xs={12} sm={6} md={3}><Stat label="Open positions" value={sum.open_positions} /></Grid>
      </Grid>
      <Snackbar open={!!msg} autoHideDuration={6000} onClose={() => setMsg("")} message={msg} />
    </Box>
  );
}
