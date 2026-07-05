import React, { useCallback, useEffect, useState } from "react";
import { api, fmt } from "../api.js";
import { Box, Typography, Button, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper, CircularProgress, Alert, Grid, Card, CardContent, Chip, Snackbar } from "@mui/material";

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

export default function PaperTrading({ userId }) {
  const [sum, setSum] = useState(null);
  const [trades, setTrades] = useState([]);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(() => {
    if (!userId) return;
    setErr("");
    Promise.all([
      api.get(`/v1/users/${userId}/paper/summary`),
      api.get(`/v1/users/${userId}/paper/trades`),
    ])
      .then(([s, t]) => { setSum(s); setTrades(t.trades || []); })
      .catch((e) => setErr(e.message));
  }, [userId]);

  useEffect(() => { load(); }, [load]);

  async function close(id) {
    try {
      await api.post(`/v1/paper/trades/${id}/close`, {});
      setMsg("Position closed.");
      load();
    } catch (e) { setMsg("Close failed: " + e.message); }
  }

  if (!userId) return <Alert severity="info">Select a user to view paper trading.</Alert>;
  if (err) return <Alert severity="error">{err}</Alert>;
  if (!sum) return (
    <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
      <CircularProgress size={24}/>
      <Typography>Loading…</Typography>
    </Box>
  );

  const pnlCls = sum.total_pnl >= 0 ? 'pos' : 'neg';

  return (
    <Box>
      <Grid container spacing={2} sx={{ mb: 3 }}>
        <Grid item xs={12} sm={6} md={4}><Stat label="Invested (open)" value={fmt(sum.invested)} /></Grid>
        <Grid item xs={12} sm={6} md={4}><Stat label="Market value" value={fmt(sum.market_value)} /></Grid>
        <Grid item xs={12} sm={6} md={4}><Stat label="Unrealized P&L" value={fmt(sum.unrealized_pnl)} cls={sum.unrealized_pnl >= 0 ? 'pos' : 'neg'} /></Grid>
        <Grid item xs={12} sm={6} md={4}><Stat label="Total P&L" value={fmt(sum.total_pnl)} cls={pnlCls} /></Grid>
        <Grid item xs={12} sm={6} md={4}><Stat label="Cash" value={fmt(sum.cash_balance)} /></Grid>
        <Grid item xs={12} sm={6} md={4}><Stat label="Equity" value={fmt(sum.equity)} /></Grid>
      </Grid>
      
      <style>{`.pos { color: #4caf50; } .neg { color: #f44336; }`}</style>

      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 'bold' }}>Positions & trade history</Typography>
        <Button variant="outlined" size="small" onClick={load}>Refresh</Button>
      </Box>

      {!trades.length ? (
        <Alert severity="info">No paper trades yet. Buy from the Scanner tab.</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell>Status</TableCell>
                <TableCell>Qty</TableCell>
                <TableCell>Entry</TableCell>
                <TableCell>Current / Exit</TableCell>
                <TableCell>P&L</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {trades.map((t) => {
                const live = t.status === "OPEN";
                const pnl = live ? t.unrealized_pnl : t.pnl;
                const pnlColor = pnl >= 0 ? 'success.main' : 'error.main';
                return (
                  <TableRow key={t.id} hover>
                    <TableCell sx={{fontWeight: 'bold'}}>{t.symbol}</TableCell>
                    <TableCell><Chip label={t.status} size="small" color={live ? 'primary' : 'default'} /></TableCell>
                    <TableCell>{t.quantity}</TableCell>
                    <TableCell>{t.entry_price ? fmt(t.entry_price) : "—"}</TableCell>
                    <TableCell>{live ? fmt(t.current_price) : t.exit_price ? fmt(t.exit_price) : "—"}</TableCell>
                    <TableCell sx={{color: pnlColor }}>{t.status === "SCHEDULED" ? "—" : fmt(pnl)}</TableCell>
                    <TableCell align="right">{live ? <Button variant="outlined" size="small" onClick={() => close(t.id)}>Close</Button> : '—'}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </TableContainer>
      )}
       <Snackbar open={!!msg} autoHideDuration={6000} onClose={() => setMsg("")} message={msg} />
    </Box>
  );
}
