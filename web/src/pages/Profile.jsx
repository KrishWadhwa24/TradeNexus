import React, { useCallback, useEffect, useState } from "react";
import { api, fmt } from "../api.js";

function Stat({ label, value, cls }) {
  return (
    <div className="card">
      <div className="label">{label}</div>
      <div className={"value " + (cls || "")}>{value}</div>
    </div>
  );
}

export default function Profile({ userId, onLogout }) {
  const [sum, setSum] = useState(null);
  const [capital, setCapital] = useState("");
  const [tg, setTg] = useState({ bot_token: "", chat_id: "", enabled: true });
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [tgMsg, setTgMsg] = useState("");

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
    setTgMsg("");
    try {
      await api.put(`/v1/users/${userId}/telegram`, tg);
      setTgMsg("Telegram config saved.");
    } catch (e) { setTgMsg("Failed: " + e.message); }
  }

  async function testTg() {
    setTgMsg("Sending test…");
    try {
      await api.post(`/v1/telegram/test`, { user_id: userId });
      setTgMsg("Test sent — check your Telegram.");
    } catch (e) { setTgMsg("Test failed: " + e.message); }
  }

  if (!userId) return <div className="empty">Sign in to view your profile.</div>;
  if (err) return <div className="err">{err}</div>;
  if (!sum) return <div className="spinner">Loading…</div>;

  return (
    <div>
      <div className="section-title">Virtual capital</div>
      <div className="panel" style={{ padding: 18, marginBottom: 24 }}>
        <div className="row">
          <input type="number" value={capital} onChange={(e) => setCapital(e.target.value)} placeholder="e.g. 100000" />
          <button className="btn-primary btn-sm" onClick={saveCapital}>Save</button>
          {msg && <span className="msg">{msg}</span>}
        </div>
        <div className="subtle" style={{ marginTop: 10 }}>
          Setting capital resets available cash to capital minus the cost of open positions.
        </div>
      </div>

      <div className="section-title">Telegram alerts</div>
      <div className="panel" style={{ padding: 18, marginBottom: 24 }}>
        <div className="grid" style={{ gridTemplateColumns: "1fr 1fr", maxWidth: 640 }}>
          <div className="field-col">
            <label className="subtle">Bot token</label>
            <input value={tg.bot_token} onChange={(e) => setTg({ ...tg, bot_token: e.target.value })} placeholder="123456:ABC-DEF…" />
          </div>
          <div className="field-col">
            <label className="subtle">Chat ID</label>
            <input value={tg.chat_id} onChange={(e) => setTg({ ...tg, chat_id: e.target.value })} placeholder="99999 or -100…" />
          </div>
        </div>
        <div className="row" style={{ marginTop: 14 }}>
          <label className="row" style={{ gap: 6 }}>
            <input type="checkbox" checked={tg.enabled} onChange={(e) => setTg({ ...tg, enabled: e.target.checked })} style={{ width: 16 }} />
            <span className="subtle">Enabled</span>
          </label>
          <button className="btn-primary btn-sm" onClick={saveTg}>Save</button>
          <button className="btn-sm" onClick={testTg}>Send test</button>
          {tgMsg && <span className="msg">{tgMsg}</span>}
        </div>
        <div className="subtle" style={{ marginTop: 10 }}>
          Tip: message your bot once (press Start) before testing, and for a channel add the bot as admin and use the <code>-100…</code> id.
        </div>
      </div>

      <div className="section-title">Profit &amp; Loss</div>
      <div className="grid cards">
        <Stat label="Starting capital" value={fmt(sum.starting_capital)} />
        <Stat label="Cash balance" value={fmt(sum.cash_balance)} />
        <Stat label="Equity" value={fmt(sum.equity)} />
        <Stat label="Total P&L" value={fmt(sum.total_pnl)} cls={sum.total_pnl >= 0 ? "pos" : "neg"} />
        <Stat label="Realized P&L" value={fmt(sum.realized_pnl)} cls={sum.realized_pnl >= 0 ? "pos" : "neg"} />
        <Stat label="Booked profit" value={fmt(sum.booked_profit)} cls="pos" />
        <Stat label="Booked loss" value={fmt(sum.booked_loss)} cls="neg" />
        <Stat label="Open positions" value={sum.open_positions} />
      </div>

      <div className="section-title" style={{ marginTop: 32 }}>Session</div>
      <div className="panel" style={{ padding: 18, marginBottom: 24 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <div>
            <div style={{ fontWeight: 600 }}>Sign out</div>
            <div className="subtle" style={{ fontSize: "0.9em", marginTop: 4 }}>End your current session and return to the login screen.</div>
          </div>
          <button className="btn-sm" onClick={onLogout} style={{ minWidth: 100 }}>
            Sign out
          </button>
        </div>
      </div>
    </div>
  );
}
