import React, { useEffect, useState } from "react";
import { api, download, fmt, fmtInt, pct } from "../api.js";
import { Box, Typography, Button, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper, CircularProgress, Alert } from "@mui/material";

export default function Analytics({ userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!userId) return;
    setLoading(true);
    setErr("");
    api
      .get(`/v1/users/${userId}/dashboard`)
      .then((r) => setRows(r.rows || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [userId]);

  function exportCsv() {
    const cols = ["symbol", "price", "pct_change", "rsi14", "ema10", "ema20", "ema50", "sma40", "atr14", "volume", "vol_sma20"];
    const header = cols.join(",");
    const lines = rows.map((r) => cols.map((c) => r[c]).join(","));
    const csv = [header, ...lines].join("\n");
    const blob = new Blob([csv], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "tradenexus_dashboard.csv";
    a.click();
    URL.revokeObjectURL(url);
  }

  if (!userId) {
      return <Alert severity="info">Select a user to view their watchlist dashboard.</Alert>;
  }
  if (loading) {
      return (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
              <CircularProgress size={24}/>
              <Typography>Loading dashboard…</Typography>
          </Box>
      );
  }
  if (err) {
      return <Alert severity="error">{err}</Alert>;
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 'bold' }}>Watchlist parameters (live price + indicators)</Typography>
        <Box sx={{ display: 'flex', gap: 1 }}>
          <Button variant="outlined" size="small" onClick={exportCsv} disabled={!rows.length}>Export CSV</Button>
          <Button variant="outlined" size="small" onClick={() => download("/v1/analytics/export.xlsx", "tradenexus_signals.xlsx").catch((e) => setErr(e.message))}>
            Export signals (.xlsx)
          </Button>
        </Box>
      </Box>

      {!rows.length ? (
        <Alert severity="warning">No watchlist stocks. Add instruments to a watchlist and sync their candles.</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table stickyHeader>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell align="right">Price</TableCell>
                <TableCell align="right">Chg%</TableCell>
                <TableCell align="right">RSI(14)</TableCell>
                <TableCell align="right">EMA10</TableCell>
                <TableCell align="right">EMA20</TableCell>
                <TableCell align="right">EMA50</TableCell>
                <TableCell align="right">SMA40</TableCell>
                <TableCell align="right">ATR(14)</TableCell>
                <TableCell align="right">Volume</TableCell>
                <TableCell align="right">Vol SMA20</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.instrument_id} hover>
                  <TableCell sx={{fontWeight: 'bold'}}>{r.symbol}</TableCell>
                  <TableCell align="right">{fmt(r.price)}</TableCell>
                  <TableCell align="right" sx={{ color: r.pct_change >= 0 ? 'success.main' : 'error.main' }}>
                    {pct(r.pct_change)}
                  </TableCell>
                  <TableCell align="right">{fmt(r.rsi14)}</TableCell>
                  <TableCell align="right">{fmt(r.ema10)}</TableCell>
                  <TableCell align="right">{fmt(r.ema20)}</TableCell>
                  <TableCell align="right">{fmt(r.ema50)}</TableCell>
                  <TableCell align="right">{fmt(r.sma40)}</TableCell>
                  <TableCell align="right">{fmt(r.atr14)}</TableCell>
                  <TableCell align="right">{fmtInt(r.volume)}</TableCell>
                  <TableCell align="right" sx={{color: 'text.secondary'}}>{fmtInt(Math.round(r.vol_sma20))}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </Box>
  );
}
