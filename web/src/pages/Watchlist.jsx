import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api.js";
import { EmptyArt } from "../icons.jsx";

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
  const [deleteTarget, setDeleteTarget] = useState(null);

  const load = useCallback(async (preferredWid = "") => {
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
      const ids = selected?.instrument_ids || [];
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
    if (!wid) return;
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

  function openDeleteModal() {
    if (!wid) return;
    setDeleteTarget(watchlists.find((w) => w.id === wid) || null);
  }

  function closeDeleteModal() {
    if (busy) return;
    setDeleteTarget(null);
  }

  async function deleteWatchlist() {
    if (!wid) return;
    setBusy(true);
    setMsg("");
    try {
      await api.del(`/v1/users/${userId}/watchlists/${wid}`);
      setMsg("Watchlist deleted.");
      setDeleteTarget(null);
      await load();
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

  if (!userId) return <div className="empty">Sign in to manage your watchlist.</div>;

  return (
    <div>
      <div className="panel" style={{ padding: 18, marginBottom: 22 }}>
        <div className="section-title" style={{ margin: "0 0 10px" }}>Watchlists</div>
        <div className="toolbar" style={{ alignItems: "flex-end", marginBottom: 14 }}>
          <label style={{ display: "grid", gap: 6 }}>
            <span className="subtle">Selected watchlist</span>
            <select value={wid} onChange={(e) => load(e.target.value)} style={{ minWidth: 220 }}>
              {watchlists.map((w) => (
                <option key={w.id} value={w.id}>{w.name}</option>
              ))}
            </select>
          </label>
          <div className="row" style={{ flexWrap: "wrap" }}>
            <button className="btn-sm btn-danger" type="button" onClick={openDeleteModal} disabled={busy || !wid}>
              Delete watchlist
            </button>
            <form className="row" onSubmit={createWatchlist}>
              <input
                style={{ width: 220 }}
                placeholder="New watchlist name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
              />
              <button className="btn-sm" type="submit" disabled={busy || !newName.trim()}>
                Create
              </button>
            </form>
          </div>
        </div>

        <div className="section-title" style={{ margin: "0 0 10px" }}>Add a stock</div>
        <input
          style={{ width: "100%", maxWidth: 460 }}
          placeholder="Search NSE/BSE stocks (e.g. RELI, TATA)…"
          value={q}
          onChange={search}
          disabled={!wid}
        />
        {busy && <span className="subtle" style={{ marginLeft: 10 }}>working…</span>}
        {msg && <div className="msg" style={{ marginTop: 8 }}>{msg}</div>}
        {results.length > 0 && (
          <div className="search-results" style={{ maxWidth: 460 }}>
            {results.map((r) => (
              <div className="search-row" key={r.id}>
                <div><b>{r.trading_symbol}</b> <span className="subtle">{r.name}</span></div>
                <button
                  className="btn-primary btn-sm pill"
                  onClick={() => add(r)}
                  disabled={busy || (activeWatchlist?.instrument_ids || []).includes(r.id)}
                >
                  {(activeWatchlist?.instrument_ids || []).includes(r.id) ? "Added" : "Add"}
                </button>
              </div>
            ))}
          </div>
        )}
        <div className="subtle" style={{ marginTop: 10 }}>
          Stocks come from the Angel scrip master. If search is empty, run scrip-master sync once.
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      <div className="section-title">{activeWatchlist?.name || "Your watchlist"}</div>
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

      {deleteTarget && (
        <div className="delete-modal-backdrop" role="presentation" onClick={closeDeleteModal}>
          <div
            className="delete-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="delete-watchlist-title"
            aria-describedby="delete-watchlist-desc"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="delete-modal-badge">Warning</div>
            <h3 id="delete-watchlist-title">Delete watchlist</h3>
            <p id="delete-watchlist-desc">
              <strong>{deleteTarget.name}</strong> will be deleted permanently.
              All stocks inside it will be removed from this watchlist, but the instruments themselves will stay in the system.
            </p>
            <div className="delete-modal-note">
              This cannot be undone.
            </div>
            <div className="delete-modal-actions">
              <button className="btn-sm btn-ghost" type="button" onClick={closeDeleteModal} disabled={busy}>
                Cancel
              </button>
              <button className="btn-sm btn-danger" type="button" onClick={deleteWatchlist} disabled={busy}>
                {busy ? "Deleting…" : "Delete watchlist"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
