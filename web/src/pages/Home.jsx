import React, { useEffect, useState } from "react";
import { api, fmt, pct } from "../api.js";
import { TrendingUp, Inbox } from "@mui/icons-material";
import { Box, Typography, Card, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper, CircularProgress, Alert } from "@mui/material";

export default function Home() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");

  useEffect(() => {
    api.get("/v1/market/trending?limit=30")
      .then((r) => setRows(r.trending || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, []);

  const Hero = () => (
    <Card sx={{
      p: {xs: 3, sm: 5},
      mb: 4,
      position: 'relative',
      background: (theme) => `
        radial-gradient(120% 120% at 100% 0%, ${theme.palette.mode === 'dark' ? 'rgba(109,110,252,.14)' : 'rgba(91,83,240,.1)'}, transparent 55%),
        ${theme.palette.background.paper}`,
    }}>
      <Box>
        <Typography variant="overline" sx={{ color: 'secondary.main' }}>
          // TRENDING_NOW
        </Typography>
        <Typography variant="h3" sx={{ mt: 1, '& .accent': { color: 'primary.main' } }}>
          Spot the movers<br />before the <span className="accent">crowd.</span>
        </Typography>
        <Typography color="text.secondary" sx={{ mt: 1.5, maxWidth: 520 }}>
          Today's top gainers across your tracked universe, ranked by daily change. Dig deeper in Analytics and the scanners.
        </Typography>
      </Box>
      <Box sx={{ position: 'absolute', right: 22, bottom: -8, width: 250, opacity: .9, pointerEvents: 'none', display: { xs: 'none', md: 'block' } }}>
        <TrendingUp sx={{ fontSize: 200, color: 'primary.main' }} />
      </Box>
    </Card>
  );

  const renderBody = () => {
    if (loading) {
      return (
          <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
              <CircularProgress size={24}/>
              <Typography>Loading trending stocks…</Typography>
          </Box>
      );
    }
    if (err) {
      return <Alert severity="error">{err}</Alert>;
    }
    if (!rows.length) {
      return (
          <Box sx={{ textAlign: 'center', p: 6 }}>
              <Box sx={{mx: 'auto', width: 110, height: 110, opacity: .7, mb: 1.5}}><Inbox sx={{ fontSize: 110 }} /></Box>
              <Typography color="text.secondary">No data yet. Add stocks to your watchlist and sync them.</Typography>
          </Box>
      );
    }
    return (
      <TableContainer component={Paper}>
        <Table sx={{ minWidth: 650 }} aria-label="simple table">
          <TableHead>
            <TableRow>
              <TableCell>#</TableCell>
              <TableCell>Symbol</TableCell>
              <TableCell align="right">Last</TableCell>
              <TableCell align="right">Prev close</TableCell>
              <TableCell align="right">Change</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((row, i) => (
              <TableRow
                key={row.instrument_id}
                sx={{ '&:last-child td, &:last-child th': { border: 0 } }}
              >
                <TableCell component="th" scope="row" sx={{color: 'text.secondary'}}>
                  {i + 1}
                </TableCell>
                <TableCell sx={{fontWeight: 'bold'}}>{row.symbol}</TableCell>
                <TableCell align="right">{fmt(row.last_close)}</TableCell>
                <TableCell align="right" sx={{color: 'text.secondary'}}>{fmt(row.prev_close)}</TableCell>
                <TableCell align="right" sx={{ color: row.pct_change >= 0 ? 'success.main' : 'error.main' }}>
                  {pct(row.pct_change)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    );
  }


  return (
    <Box>
      <Hero />
      <Typography variant="h5" sx={{ mb: 2, fontWeight: 'bold' }}>Top movers today</Typography>
      {renderBody()}
    </Box>
  );
}
