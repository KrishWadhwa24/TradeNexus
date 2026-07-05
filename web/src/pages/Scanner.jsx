import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api.js";
import { Box, Typography, Button, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper, CircularProgress, Alert, TextField, Chip, Snackbar } from "@mui/material";

// source: "pine" | "weekly"
export default function Scanner({ source, userId }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [qty, setQty] = useState({});

  const load = useCallback(() => {
    setLoading(true);
    setErr("");
    api
      .get(`/v1/signals?source=${source}&limit=300`)
      .then((r) => {
        const cutoff = Date.now() - 7 * 24 * 3600 * 1000; // last 7 days
        setRows((r.signals || []).filter((s) => new Date(s.created_at).getTime() >= cutoff));
      })
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [source]);

  useEffect(() => { load(); }, [load]);

  async function runScan() {
    setMsg("Running scan on all tracked stocks…");
    try {
      const r = await api.post("/v1/admin/scan-all", {});
      setMsg(`Scan complete (${r.count} stocks). Refreshing…`);
      load();
    } catch (e) {
      setMsg("Scan failed: " + e.message);
    }
  }

  async function buy(sig) {
    if (!userId) { setMsg("Select a user first (top right)."); return; }
    const q = parseInt(qty[sig.id] || "1", 10);
    try {
      const t = await api.post(`/v1/users/${userId}/paper/trades`, { signal_id: sig.id, quantity: q });
      setMsg(`Paper ${t.status === "SCHEDULED" ? "trade scheduled for next open" : "bought"}: ${t.symbol} x${t.quantity}`);
    } catch (e) {
      setMsg("Buy failed: " + e.message);
    }
  }

  if (loading) return (
      <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
          <CircularProgress size={24}/>
          <Typography>Loading signals…</Typography>
      </Box>
  );
  if (err) return <Alert severity="error">{err}</Alert>;

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 'bold' }}>
          {source === "pine" ? "Pine (Chase Momentum)" : "Weekly scanners"} — current & last 7 days
        </Typography>
        <Box sx={{ display: 'flex', gap: 1, alignItems: 'center' }}>
          <Button variant="outlined" size="small" onClick={runScan}>Run scan now</Button>
          <Button variant="outlined" size="small" onClick={load}>Refresh</Button>
        </Box>
      </Box>

      {!rows.length ? (
        <Alert severity="info">No signals in the last 7 days.</Alert>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell>Signal</TableCell>
                <TableCell>Timeframe</TableCell>
                {source === "weekly" && <TableCell>Confidence</TableCell>}
                <TableCell>Scanner(s)</TableCell>
                <TableCell>Candle date</TableCell>
                <TableCell align="right">Buy</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((s) => (
                <TableRow key={s.id} hover>
                  <TableCell sx={{fontWeight: 'bold'}}>{s.symbol}</TableCell>
                  <TableCell>
                    <Chip
                      label={s.direction}
                      color={s.direction === "BUY" ? "success" : "error"}
                      size="small"
                    />
                  </TableCell>
                  <TableCell>{s.timeframe}</TableCell>
                  {source === "weekly" && <TableCell>{s.confidence != null ? s.confidence + "/4" : "—"}</TableCell>}
                  <TableCell sx={{color: 'text.secondary'}}>{s.scanner_name}</TableCell>
                  <TableCell sx={{color: 'text.secondary'}}>{s.candle_date?.slice(0, 10)}</TableCell>
                  <TableCell align="right">
                    {s.direction === "BUY" ? (
                      <Box sx={{ display: 'flex', gap: 1, justifyContent: "flex-end" }}>
                        <TextField
                          type="number"
                          size="small"
                          defaultValue="1"
                          onChange={(e) => setQty({ ...qty, [s.id]: e.target.value })}
                          sx={{ width: 80 }}
                          InputProps={{ inputProps: { min: 1 } }}
                        />
                        <Button variant="contained" size="small" onClick={() => buy(s)}>Buy</Button>
                      </Box>
                    ) : '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
      <Snackbar open={!!msg} autoHideDuration={6000} onClose={() => setMsg("")} message={msg} />
    </Box>
  );
}
