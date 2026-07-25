import React, { useState } from "react";
import { authApi, setToken } from "../api.js";

export default function Login({ onAuthed, onBack }) {
  const [mode, setMode] = useState("login"); // login | register
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setErr("");
    setBusy(true);
    try {
      const fn = mode === "login" ? authApi.login : authApi.register;
      const r = await fn(email, password);
      setToken(r.token);
      onAuthed(r.user);
    } catch (e2) {
      setErr(e2.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth-wrap">
      <div className="auth-art">
        <div className="brand" style={{ padding: 0, cursor: onBack ? "pointer" : "default" }} onClick={onBack}>
          <span className="prompt">&gt;_</span> Trade<em>Nexus</em>
        </div>
        {onBack && (
          <a className="subtle" style={{ cursor: "pointer", display: "inline-block", marginTop: 6 }} onClick={onBack}>← Back to home</a>
        )}
        <span className="kicker">// UNIFIED_STOCK_SCANNER</span>
        <h1>Find winning stocks<br />before the <span className="accent">crowd.</span></h1>
        <p>Pine &amp; weekly scanners over NSE + BSE data, live analytics, Telegram alerts, and paper trading — all in one terminal.</p>
      </div>

      <form className="auth-form" onSubmit={submit}>
        <h2>{mode === "login" ? "Welcome back" : "Create your account"}</h2>
        <div className="subtle">{mode === "login" ? "Sign in to your dashboard." : "Start scanning in seconds."}</div>

        <div className="field">
          <label>Email</label>
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" required />
        </div>
        <div className="field">
          <label>Password</label>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="min 6 characters" required />
        </div>

        {err && <div className="err">{err}</div>}

        <button className="btn-primary" disabled={busy}>
          {busy ? "Please wait…" : mode === "login" ? "Sign in" : "Create account"}
        </button>

        <div className="auth-switch">
          {mode === "login" ? (
            <>New here? <a onClick={() => { setMode("register"); setErr(""); }}>Create an account</a></>
          ) : (
            <>Already have an account? <a onClick={() => { setMode("login"); setErr(""); }}>Sign in</a></>
          )}
        </div>
      </form>
    </div>
  );
}
