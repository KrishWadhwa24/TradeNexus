import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api.js";
import { EmptyArt } from "../icons.jsx";

export default function Watchlist({ userId }) {
  const [wid, setWid] = useState("");
  const [items, setItems] = useState([]); // instrument detail objects
  const [q, setQ] = useState("");
  const [results, setResults] = useState([]);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  // Ensure the user has a watchlist, then load its items (with symbols).
  const load = useCallback(async () => {
    setErr("");
    try {
      let wls = (await api.get(`/v1/users/${userId}/watchlists`)).watchlists || [];
      if (!wls.length) {
        await api.post(`/v1/users/${userId}/watchlists`, { name: "My Watchlist" });
        wls = (await api.get(`/v1/users/${userId}/watchlists`)).watchlists || [];
      }
      const w = wls[0];
      setWid(w.id);
      const ids = w.instrument_ids || [];
      const details = await Promise.all(
        ids.map((id) => api.get(`/v1/instruments/${id}`).catch(() => ({ id })))
      );
      setItems(details);
    } catch (e) {
      setErr(e.message);
    }
  }, [userId]);

  useEffect(() => { if (userId) load(); }, [userId, load]);

  async function search(e) {
    const v = e.target.value;
    setQ(v);
    if (v.trim().length < 1) { setResults([]); return; }
    try {
      const r = await api.get(`/v1/instruments/search?q=${encodeURIComponent(v)}&limit=12`);
      setResults(r.instruments || []);
    } catch { setResults([]); }
  }

  async function add(inst) {
    setBusy(true);
    setMsg("");
    try {
      await api.post(`/v1/watchlists/${wid}/items`, { instrument_id: inst.id });
      setMsg(`Added ${inst.trading_symbol}. Fetching history…`);
      // Pull daily history + build weekly/monthly so scanners have data.
      await api.post(`/v1/instruments/${inst.id}/candles/sync?days=1300`);
      setMsg(`${inst.trading_symbol} added and synced.`);
      setQ(""); setResults([]);
      load();
    } catch (e) {
      setMsg("Failed: " + e.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove(id) {
    await api.del(`/v1/watchlists/${wid}/items/${id}`);
    load();
  }

  if (!userId) return <div className="empty">Sign in to manage your watchlist.</div>;

  return (
    <div>
      <div className="panel" style={{ padding: 18, marginBottom: 22 }}>
        <div className="section-title" style={{ margin: "0 0 10px" }}>Add a stock</div>
        <input
          style={{ width: "100%", maxWidth: 460 }}
          placeholder="Search NSE stocks (e.g. RELI, TATA)…"
          value={q}
          onChange={search}
        />
        {busy && <span className="subtle" style={{ marginLeft: 10 }}>working…</span>}
        {msg && <div className="msg" style={{ marginTop: 8 }}>{msg}</div>}
        {results.length > 0 && (
          <div className="search-results" style={{ maxWidth: 460 }}>
            {results.map((r) => (
              <div className="search-row" key={r.id}>
                <div><b>{r.trading_symbol}</b> <span className="subtle">{r.name}</span></div>
                <button className="btn-primary btn-sm pill" onClick={() => add(r)}>Add</button>
              </div>
            ))}
          </div>
        )}
        <div className="subtle" style={{ marginTop: 10 }}>
          Stocks come from the Angel scrip master. If search is empty, run scrip-master sync once.
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      <div className="section-title">Your stocks</div>
      {!items.length ? (
        <div className="empty"><EmptyArt /><div>No stocks yet. Search above to add your first.</div></div>
      ) : (
        <div className="panel">
          <table>
            <thead>
              <tr><th>Symbol</th><th>Name</th><th>Exchange</th><th></th></tr>
            </thead>
            <tbody>
              {items.map((it) => (
                <tr key={it.id}>
                  <td><b>{it.trading_symbol || `#${it.id}`}</b></td>
                  <td className="muted">{it.name || "—"}</td>
                  <td className="muted">{it.exchange || "—"}</td>
                  <td><button className="btn-sm" onClick={() => remove(it.id)}>Remove</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
