import React, { useState } from "react";
import { api } from "../api.js";

// Admin-only candle tools: inspect / delete / refetch a specific trading day.
// Visible only when the signed-in user has is_admin (gated again server-side).
export default function Admin() {
  const today = new Date().toISOString().slice(0, 10);
  const [date, setDate] = useState(today);
  const [info, setInfo] = useState(null);   // {date, weekday, is_trading_day, count}
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");     // "" | "count" | "delete" | "refetch"
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Stock candle lookup
  const [sq, setSq] = useState("");
  const [sresults, setSresults] = useState([]);
  const [cov, setCov] = useState(null);
  const [covErr, setCovErr] = useState("");
  const [covBusy, setCovBusy] = useState(false);

  async function check() {
    setBusy("count"); setErr(""); setMsg("");
    try {
      const r = await api.get(`/v1/admin/candles?date=${date}`);
      setInfo(r);
    } catch (e) { setErr(e.message); setInfo(null); }
    finally { setBusy(""); }
  }

  async function del() {
    setBusy("delete"); setErr(""); setMsg(""); setConfirmDelete(false);
    try {
      const r = await api.del(`/v1/admin/candles?date=${date}`);
      setMsg(`Deleted ${r.deleted} candle row(s) for ${r.date}. Aggregates rebuilt.`);
      await check();
    } catch (e) { setErr(e.message); }
    finally { setBusy(""); }
  }

  async function refetch() {
    setBusy("refetch"); setErr(""); setMsg("");
    try {
      const r = await api.post(`/v1/admin/candles/refetch?date=${date}`, {});
      if (r.status === "already_running") {
        setMsg("A refetch is already running — hang tight.");
      } else {
        setMsg(`Refetch started for ${r.date} in the background. Re-check the count in a minute to confirm.`);
      }
    } catch (e) { setErr(e.message); }
    finally { setBusy(""); }
  }

  async function searchStock(e) {
    const v = e.target.value;
    setSq(v);
    if (v.trim().length < 1) { setSresults([]); return; }
    try {
      const r = await api.get(`/v1/instruments/search?q=${encodeURIComponent(v)}&limit=12`);
      setSresults(r.instruments || []);
    } catch { setSresults([]); }
  }

  async function lookup(inst) {
    setCovBusy(true); setCovErr(""); setCov(null);
    setSq(inst.trading_symbol); setSresults([]);
    try {
      const c = await api.get(`/v1/instruments/${inst.id}/coverage`);
      setCov({ ...c, _symbol: inst.trading_symbol, _name: inst.name, _exchange: inst.exchange });
    } catch (e) { setCovErr(e.message); }
    finally { setCovBusy(false); }
  }

  return (
    <div>
      <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
        <div className="section-title" style={{ margin: "0 0 6px" }}>Stock candle lookup</div>
        <div className="subtle" style={{ marginBottom: 14 }}>
          Search a stock to see how many candles are stored for it and the date range covered.
        </div>
        <input
          style={{ width: "100%", maxWidth: 460 }}
          placeholder="Search NSE/BSE stocks (e.g. RELI, TATA)…"
          value={sq}
          onChange={searchStock}
        />
        {covBusy && <span className="subtle" style={{ marginLeft: 10 }}>loading…</span>}
        {sresults.length > 0 && (
          <div className="search-results" style={{ maxWidth: 460 }}>
            {sresults.map((r) => (
              <div className="search-row" key={r.id}>
                <div><b>{r.trading_symbol}</b> <span className="subtle">{r.name}</span></div>
                <button className="btn-sm pill" onClick={() => lookup(r)}>Lookup</button>
              </div>
            ))}
          </div>
        )}
        {covErr && <div className="err" style={{ marginTop: 12 }}>{covErr}</div>}
        {cov && (
          <div style={{ marginTop: 16 }}>
            <div className="section-title" style={{ margin: "0 0 10px" }}>
              {cov._symbol} <span className="subtle">{cov._exchange}{cov._name ? " · " + cov._name : ""}</span>
            </div>
            <div className="cards">
              <div className="card"><div className="label">Daily candles</div><div className="value">{cov.daily_candles ?? 0}</div></div>
              <div className="card"><div className="label">Weekly</div><div className="value">{cov.weekly_candles ?? 0}</div></div>
              <div className="card"><div className="label">Monthly</div><div className="value">{cov.monthly_candles ?? 0}</div></div>
              <div className="card"><div className="label">Missing days</div><div className="value">{cov.missing_trading_days ?? 0}</div></div>
            </div>
            <div className="subtle" style={{ marginTop: 12 }}>
              {cov.has_data
                ? `Range: ${cov.first_date || "—"} → ${cov.last_date || "—"} (target ${cov.target_daily_bars} bars)`
                : "No candles stored yet for this stock."}
            </div>
          </div>
        )}
      </div>

      <div className="panel" style={{ padding: 20, marginBottom: 20 }}>
        <div className="section-title" style={{ margin: "0 0 6px" }}>Candle tools</div>
        <div className="subtle" style={{ marginBottom: 16 }}>
          Inspect, delete, or re-fetch the stored daily candles for a single trading day.
          Deleting a day removes it for every stock; refetch pulls the finalized bar from Angel
          (only works once that session has closed).
        </div>

        <div className="toolbar" style={{ alignItems: "flex-end" }}>
          <label style={{ display: "grid", gap: 6 }}>
            <span className="subtle">Trading day</span>
            <input type="date" value={date} max={today} onChange={(e) => setDate(e.target.value)} />
          </label>
          <div className="row" style={{ flexWrap: "wrap" }}>
            <button className="btn-sm" onClick={check} disabled={!!busy}>
              {busy === "count" ? "Checking…" : "Check count"}
            </button>
            <button className="btn-sm btn-danger" onClick={() => setConfirmDelete(true)} disabled={!!busy}>
              {busy === "delete" ? "Deleting…" : "Delete day"}
            </button>
            <button className="btn-sm btn-primary" onClick={refetch} disabled={!!busy}>
              {busy === "refetch" ? "Starting…" : "Refetch day"}
            </button>
          </div>
        </div>

        {msg && <div className="msg" style={{ marginTop: 12 }}>{msg}</div>}
        {err && <div className="err" style={{ marginTop: 12 }}>{err}</div>}
      </div>

      {info && (
        <div className="panel" style={{ padding: 20 }}>
          <div className="section-title" style={{ margin: "0 0 12px" }}>{info.date}</div>
          <div className="cards">
            <div className="card">
              <div className="label">Stored candles</div>
              <div className="value">{info.count}</div>
            </div>
            <div className="card">
              <div className="label">Weekday</div>
              <div className="value" style={{ fontSize: 20 }}>{info.weekday}</div>
            </div>
            <div className="card">
              <div className="label">Trading day?</div>
              <div className="value" style={{ fontSize: 20 }}>
                <span className={info.is_trading_day ? "tag tag-buy" : "tag tag-sell"}>
                  {info.is_trading_day ? "YES" : "NO"}
                </span>
              </div>
            </div>
          </div>
          {info.is_trading_day && info.count === 0 && (
            <div className="subtle" style={{ marginTop: 14 }}>
              No candles stored for this trading day — a gap. Use “Refetch day” once the session has closed to backfill it.
            </div>
          )}
        </div>
      )}

      {confirmDelete && (
        <div className="delete-modal-backdrop" onClick={() => setConfirmDelete(false)}>
          <div className="delete-modal" onClick={(e) => e.stopPropagation()}>
            <span className="delete-modal-badge">Destructive</span>
            <h3>Delete {date}?</h3>
            <p>
              This removes the stored daily candle for <b>every</b> instrument on {date} and rebuilds
              their weekly/monthly aggregates. You can refetch it afterwards if the session has closed.
            </p>
            <div className="row" style={{ justifyContent: "flex-end", marginTop: 18 }}>
              <button className="btn-sm btn-ghost" onClick={() => setConfirmDelete(false)}>Cancel</button>
              <button className="btn-sm btn-danger" onClick={del}>Delete day</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
