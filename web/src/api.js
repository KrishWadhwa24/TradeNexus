// API client with JWT auth. Token is attached to every request; a 401 clears
// it and broadcasts "auth-expired" so the app can bounce to the login screen.
let token = localStorage.getItem("token") || "";

export function setToken(t) {
  token = t || "";
  if (t) localStorage.setItem("token", t);
  else localStorage.removeItem("token");
}
export function getToken() {
  return token;
}

const API_BASE = import.meta.env.VITE_API_BASE_URL || "";

export function livePricesURL(userId) {
  const base = import.meta.env.VITE_API_BASE_URL || window.location.origin;
  const wsBase = base.replace(/^http/, "ws");
  //const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const qs = new URLSearchParams({ token });
  return `${wsBase}/v1/users/${userId}/live-prices?${qs}`;
}

// publicLivePricesURL is the pre-login landing page's equivalent — no auth,
// no user id. Must also respect VITE_API_BASE_URL (the backend is a separate
// deployment from this static frontend), not just window.location.host.
export function publicLivePricesURL() {
  const base = import.meta.env.VITE_API_BASE_URL || window.location.origin;
  const wsBase = base.replace(/^http/, "ws");
  return `${wsBase}/v1/public/live-prices`;
}

// connectWebSocketWithRetry is the shared auto-reconnect wrapper behind
// every live-tick websocket in this app (equity live-prices, the option
// chain stream) — urlFn is called fresh on every (re)connect so a token
// refresh or a changed subscription list is picked up.
function connectWebSocketWithRetry(urlFn, { onMessage, onOpen, onClose, onError } = {}) {
  let ws = null;
  let retryTimer = null;
  let disposed = false;
  let retryDelay = 1000;

  const clearRetry = () => {
    if (retryTimer) {
      window.clearTimeout(retryTimer);
      retryTimer = null;
    }
  };

  const scheduleReconnect = () => {
    if (disposed) return;
    clearRetry();
    retryTimer = window.setTimeout(connect, retryDelay);
    retryDelay = Math.min(retryDelay * 2, 30000);
  };

  function connect() {
    if (disposed) return;
    clearRetry();
    ws = new WebSocket(urlFn());
    ws.onopen = (event) => {
      retryDelay = 1000;
      onOpen?.(event);
    };
    ws.onmessage = (event) => onMessage?.(event, ws);
    ws.onerror = (event) => {
      onError?.(event);
    };
    ws.onclose = (event) => {
      onClose?.(event);
      scheduleReconnect();
    };
  }

  connect();

  return () => {
    disposed = true;
    clearRetry();
    if (ws) ws.close();
  };
}

export function connectLivePrices(userId, handlers) {
  return connectWebSocketWithRetry(() => livePricesURL(userId), handlers);
}

// optionChainStreamURL builds the URL for the live option-chain tick stream
// (bid/ask/volume/OI, SnapQuote mode) — ids are the instrument IDs the
// frontend already has from GET /optionsalgo/chain.
export function optionChainStreamURL(userId, ids) {
  const base = import.meta.env.VITE_API_BASE_URL || window.location.origin;
  const wsBase = base.replace(/^http/, "ws");
  const qs = new URLSearchParams({ token, ids: ids.join(",") });
  return `${wsBase}/v1/users/${userId}/optionsalgo/chain-stream?${qs}`;
}

export function connectOptionChainStream(userId, ids, handlers) {
  return connectWebSocketWithRetry(() => optionChainStreamURL(userId, ids), handlers);
}

async function req(method, path, body) {
  const opts = { method, headers: {} };
  if (token) opts.headers["Authorization"] = "Bearer " + token;
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(API_BASE + path, opts);

  if (res.status === 401) {
    setToken("");
    window.dispatchEvent(new Event("auth-expired"));
    throw new Error("session expired");
  }

  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!res.ok) {
    // Surface the most specific detail we have: server {error}, raw body text,
    // or the HTTP status text — never a bare "request failed" that hides the
    // cause, but also never a raw "HTTP 400:" prefix in front of it — that's
    // noise to a user, not a useful detail.
    const detail =
      (data && data.error) ||
      (typeof data === "string" && data.trim()) ||
      res.statusText ||
      `request failed (${res.status})`;
    throw new Error(detail);
  }
  return data;
}

export const api = {
  get: (p) => req("GET", p),
  post: (p, b) => req("POST", p, b),
  put: (p, b) => req("PUT", p, b),
  del: (p) => req("DELETE", p),
};

// download fetches a file with the auth header and saves it (browser <a href>
// links can't send Authorization, so we must fetch the blob ourselves).
export async function download(path, filename) {
  const res = await fetch(API_BASE + path, {
    headers: token ? { Authorization: "Bearer " + token } : {},
  });
  if (res.status === 401) {
    setToken("");
    window.dispatchEvent(new Event("auth-expired"));
    throw new Error("session expired");
  }
  if (!res.ok) throw new Error("download failed");
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export const authApi = {
  login: (email, password) => req("POST", "/v1/auth/login", { email, password }),
  register: (email, password) => req("POST", "/v1/auth/register", { email, password }),
  google: (idToken) => req("POST", "/v1/auth/google", { id_token: idToken }),
};

// convLevel maps a signal's confidence to "low" | "med" | "high" so the UI can
// colour-code it. Weekly scanners are on a 1–4 scale; pattern conviction is
// 0–100. Returns null when there's no value to show.
export function convLevel(source, value) {
  if (value === null || value === undefined) return null;
  if (source === "weekly") return value >= 3 ? "high" : value === 2 ? "med" : "low";
  return value >= 70 ? "high" : value >= 40 ? "med" : "low"; // patterns / conviction
}

// convLabel is the text shown inside the badge for a given source.
export function convLabel(source, value) {
  if (value === null || value === undefined) return "—";
  if (source === "weekly") return value + "/4";
  return value + "%"; // pattern conviction
}

// Formatting helpers.
export const fmt = (n, d = 2) =>
  n === null || n === undefined || isNaN(n) ? "—" : Number(n).toFixed(d);
export const fmtInt = (n) =>
  n === null || n === undefined ? "—" : Number(n).toLocaleString();
export const pct = (n) => (n >= 0 ? "+" : "") + fmt(n) + "%";
