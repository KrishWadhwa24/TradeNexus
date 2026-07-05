import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api.js";
import {
    Box, Typography, Select, MenuItem, TextField, Button, List, ListItem, ListItemText,
    Table, TableBody, TableCell, TableContainer, TableHead, TableRow, Paper,
    CircularProgress, Alert, Card, Grid, InputLabel, FormControl, Snackbar, IconButton, Autocomplete
} from "@mui/material";
import { Add, Delete, Inbox } from "@mui/icons-material";

export default function Watchlist({ userId }) {
  const [watchlists, setWatchlists] = useState([]);
  const [wid, setWid] = useState("");
  const [items, setItems] = useState([]); // instrument detail objects
  const [newName, setNewName] = useState("");
  const [q, setQ] = useState("");
  const [results, setResults] = useState([]);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async (preferredWid = "") => {
    setLoading(true);
    setErr("");
    try {
      let wls = (await api.get(`/v1/users/${userId}/watchlists`)).watchlists || [];
      if (!wls.length) {
        await api.post(`/v1/users/${userId}/watchlists`, { name: "My Watchlist" });
        wls = (await api.get(`/v1/users/${userId}/watchlists`)).watchlists || [];
      }
      setWatchlists(wls);

      const selected = wls.find((w) => w.id === preferredWid) || wls[0];
      setWid(selected?.id || "");
      if (selected) {
        const ids = selected.instrument_ids || [];
        const details = await Promise.all(
          ids.map((id) => api.get(`/v1/instruments/${id}`).catch(() => ({ id })))
        );
        setItems(details);
      } else {
        setItems([]);
      }
    } catch (e) {
      setErr(e.message);
    } finally {
      setLoading(false);
    }
  }, [userId]);

  useEffect(() => { if (userId) load(); }, [userId, load]);

  async function search(event, value) {
    setQ(value);
    if (value.trim().length < 1) { setResults([]); return; }
    try {
      const r = await api.get(`/v1/instruments/search?q=${encodeURIComponent(value)}&limit=12`);
      setResults(r.instruments || []);
    } catch { setResults([]); }
  }

  async function add(inst) {
    if (!wid || !inst) return;
    setBusy(true);
    setMsg("");
    try {
      await api.post(`/v1/watchlists/${wid}/items`, { instrument_id: inst.id });
      const cov = await api.get(`/v1/instruments/${inst.id}/coverage`);
      if (cov.has_data) {
        setMsg(`${inst.trading_symbol} added. History already exists, so sync was skipped.`);
      } else {
        setMsg(`Added ${inst.trading_symbol}. Fetching history…`);
        await api.post(`/v1/instruments/${inst.id}/candles/sync?days=1300`);
        setMsg(`${inst.trading_symbol} added and synced.`);
      }
      setQ(""); setResults([]);
      load(wid);
    } catch (e) {
      setMsg("Failed: " + e.message);
    } finally {
      setBusy(false);
    }
  }

  async function createWatchlist(e) {
    e.preventDefault();
    const name = newName.trim();
    if (!name) return;
    setBusy(true);
    setMsg("");
    try {
      const w = await api.post(`/v1/users/${userId}/watchlists`, { name });
      setNewName("");
      setMsg(`Created ${w.name}.`);
      await load(w.id);
    } catch (e) {
      setMsg("Failed: " + e.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove(id) {
    await api.del(`/v1/watchlists/${wid}/items/${id}`);
    load(wid);
  }

  const activeWatchlist = watchlists.find((w) => w.id === wid);

  if (!userId) return <Alert severity="info">Sign in to manage your watchlist.</Alert>;
  if (loading) return <CircularProgress />;

  return (
    <Box>
        {err && <Alert severity="error" sx={{mb: 2}}>{err}</Alert>}
        <Card sx={{ p: 2, mb: 3 }}>
            <Typography variant="h6" sx={{ mb: 2 }}>Watchlists</Typography>
            <Grid container spacing={2} alignItems="flex-end">
                <Grid item xs={12} md={6}>
                    <FormControl fullWidth>
                        <InputLabel id="watchlist-select-label">Selected Watchlist</InputLabel>
                        <Select labelId="watchlist-select-label" value={wid} label="Selected Watchlist" onChange={(e) => load(e.target.value)}>
                            {watchlists.map((w) => <MenuItem key={w.id} value={w.id}>{w.name}</MenuItem>)}
                        </Select>
                    </FormControl>
                </Grid>
                <Grid item xs={12} md={6}>
                    <Box component="form" sx={{ display: 'flex', gap: 1 }} onSubmit={createWatchlist}>
                        <TextField
                            fullWidth
                            label="New watchlist name"
                            value={newName}
                            onChange={(e) => setNewName(e.target.value)}
                            size="small"
                        />
                        <Button variant="contained" type="submit" disabled={busy || !newName.trim()}>Create</Button>
                    </Box>
                </Grid>
            </Grid>
            <Box sx={{ mt: 3 }}>
                <Typography variant="h6" sx={{ mb: 1 }}>Add a stock</Typography>
                <Autocomplete
                    fullWidth
                    freeSolo
                    options={results}
                    getOptionLabel={(option) => option.trading_symbol || ""}
                    onInputChange={search}
                    onChange={(event, newValue) => { add(newValue); }}
                    disabled={!wid || busy}
                    renderInput={(params) => <TextField {...params} label="Search NSE/BSE stocks (e.g. RELI, TATA)…" />}
                    renderOption={(props, option) => (
                        <ListItem {...props} key={option.id} secondaryAction={
                            <Button
                                size="small"
                                variant="contained"
                                disabled={busy || activeWatchlist?.instrument_ids?.includes(option.id)}
                            >
                                {activeWatchlist?.instrument_ids?.includes(option.id) ? "Added" : "Add"}
                            </Button>
                        }>
                            <ListItemText primary={option.trading_symbol} secondary={option.name} />
                        </ListItem>
                    )}
                />
                 {busy && <CircularProgress size={20} sx={{ml: 2}} />}
                 <Typography variant="caption" color="text.secondary" sx={{mt: 1, display: 'block'}}>
                    Stocks come from the Angel scrip master. If search is empty, run scrip-master sync once.
                 </Typography>
            </Box>
        </Card>

      <Typography variant="h5" sx={{ mb: 2, fontWeight: 'bold' }}>{activeWatchlist?.name || "Your watchlist"}</Typography>
      {!items.length ? (
        <Box sx={{ textAlign: 'center', p: 6 }}>
            <Box sx={{mx: 'auto', width: 110, height: 110, opacity: .7, mb: 1.5}}><Inbox sx={{ fontSize: 110 }} /></Box>
            <Typography color="text.secondary">No stocks yet. Search above to add your first.</Typography>
        </Box>
      ) : (
        <TableContainer component={Paper}>
          <Table>
            <TableHead>
              <TableRow>
                <TableCell>Symbol</TableCell>
                <TableCell>Name</TableCell>
                <TableCell>Exchange</TableCell>
                <TableCell align="right">Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((it) => (
                <TableRow key={it.id} hover>
                  <TableCell sx={{fontWeight: 'bold'}}>{it.trading_symbol || `#${it.id}`}</TableCell>
                  <TableCell>{it.name || "—"}</TableCell>
                  <TableCell>{it.exchange || "—"}</TableCell>
                  <TableCell align="right">
                      <IconButton size="small" onClick={() => remove(it.id)}><Delete /></IconButton>
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
