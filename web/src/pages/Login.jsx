import React, { useEffect, useRef, useState } from "react";
import { authApi, setToken } from "../api.js";

const GOOGLE_CLIENT_ID = import.meta.env.VITE_GOOGLE_CLIENT_ID || "";

export default function Login({ onAuthed, onBack }) {
  const [mode, setMode] = useState("login"); // login | register
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const googleBtnRef = useRef(null);

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

  async function onGoogleCredential(response) {
    setErr("");
    setBusy(true);
    try {
      const r = await authApi.google(response.credential);
      setToken(r.token);
      onAuthed(r.user);
    } catch (e2) {
      setErr(e2.message);
    } finally {
      setBusy(false);
    }
  }

  // Google Identity Services renders the button itself (its own styling,
  // ID-token flow) — loaded as a plain script tag rather than an npm package,
  // so this stays a zero-new-dependency feature on both ends.
  useEffect(() => {
    if (!GOOGLE_CLIENT_ID) return;

    function render() {
      if (!window.google || !googleBtnRef.current) return;
      window.google.accounts.id.initialize({
        client_id: GOOGLE_CLIENT_ID,
        callback: onGoogleCredential,
      });
      window.google.accounts.id.renderButton(googleBtnRef.current, {
        theme: "outline",
        size: "large",
        width: 320,
      });
    }

    if (window.google) {
      render();
      return;
    }
    const script = document.createElement("script");
    script.src = "https://accounts.google.com/gsi/client";
    script.async = true;
    script.onload = render;
    document.body.appendChild(script);
    return () => { document.body.removeChild(script); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

        {GOOGLE_CLIENT_ID && (
          <>
            <div className="auth-divider"><span>or</span></div>
            <div ref={googleBtnRef} style={{ display: "flex", justifyContent: "center" }} />
          </>
        )}

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
