import React, { useEffect, useState } from "react";
import { api } from "../api.js";
import { Box, Typography, Button, Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper, CircularProgress, Alert, FormControl, InputLabel, Select, MenuItem, Chip } from "@mui/material";

export default function Audit() {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [tf, setTf] = useState("");
  const [source, setSource] = useState("");

  function load() {
    setLoading(true);
    setErr("");
    const q = new URLSearchParams({ limit: "500" });
    if (tf) q.set("tf", tf);
    if (source) q.set("source", source);
    api
      .get("/v1/signals?" + q.toString())
      .then((r) => setRows(r.signals || []))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }

  useEffect(load, [tf, source]);

  const renderBody = () => {
    if (loading) return (
        <Box sx={{ display: 'flex', justifyContent: 'center', p: 5, gap: 2, alignItems: 'center' }}>
            <CircularProgress size={24}/>
            <Typography>Loading audit…</Typography>
        </Box>
    );
    if (err) return <Alert severity="error">{err}</Alert>;
    if (!rows.length) return <Alert severity="info">No signals recorded.</Alert>;

    return (
      <TableContainer component={Paper}>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Symbol</TableCell>
              <TableCell>Signal</TableCell>
              <TableCell>Source</TableCell>
              <TableCell>Timeframe</TableCell>
              <TableCell>Confidence</TableCell>
              <TableCell>Scanner(s)</TableCell>
              <TableCell>Candle date</TableCell>
              <TableCell>Generated</TableCell>
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
                <TableCell sx={{color: 'text.secondary'}}>{s.source}</TableCell>
                <TableCell>{s.timeframe}</TableCell>
                <TableCell>{s.confidence != null ? s.confidence + "/4" : "—"}</TableCell>
                <TableCell sx={{color: 'text.secondary'}}>{s.scanner_name}</TableCell>
                <TableCell sx={{color: 'text.secondary'}}>{s.candle_date?.slice(0, 10)}</TableCell>
                <TableCell sx={{color: 'text.secondary'}}>{new Date(s.created_at).toLocaleString()}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    );
  }

  return (
    <Box>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2, flexWrap: 'wrap', gap: 2 }}>
        <Typography variant="h5" sx={{ fontWeight: 'bold' }}>
          All signals (retained 30 days, then auto-removed)
        </Typography>
        <Box sx={{ display: 'flex', gap: 2 }}>
          <FormControl size="small" sx={{minWidth: 150}}>
            <InputLabel>Source</InputLabel>
            <Select value={source} label="Source" onChange={(e) => setSource(e.target.value)}>
              <MenuItem value="">All sources</MenuItem>
              <MenuItem value="pine">Pine</MenuItem>
              <MenuItem value="weekly">Weekly</MenuItem>
            </Select>
          </FormControl>
          <FormControl size="small" sx={{minWidth: 150}}>
            <InputLabel>Timeframe</InputLabel>
            <Select value={tf} label="Timeframe" onChange={(e) => setTf(e.target.value)}>
              <MenuItem value="">All timeframes</MenuItem>
              <MenuItem value="1D">1D</MenuItem>
              <MenuItem value="1W">1W</MenuItem>
              <MenuItem value="1M">1M</MenuItem>
            </Select>
          </FormControl>
          <Button variant="outlined" onClick={load}>Refresh</Button>
        </Box>
      </Box>
      {renderBody()}
    </Box>
  );
}
