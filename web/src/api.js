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

export function livePricesURL(userId) {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const qs = new URLSearchParams({ token });
  return `${proto}//${window.location.host}/v1/users/${userId}/live-prices?${qs}`;
}

export function connectLivePrices(userId, { onMessage, onOpen, onClose, onError } = {}) {
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
    ws = new WebSocket(livePricesURL(userId));
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

async function req(method, path, body) {
  const opts = { method, headers: {} };
  if (token) opts.headers["Authorization"] = "Bearer " + token;
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);

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
  if (!res.ok) throw new Error((data && data.error) || res.statusText || "request failed");
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
  const res = await fetch(path, {
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
};

// Formatting helpers.
export const fmt = (n, d = 2) =>
  n === null || n === undefined || isNaN(n) ? "—" : Number(n).toFixed(d);
export const fmtInt = (n) =>
  n === null || n === undefined ? "—" : Number(n).toLocaleString();
export const pct = (n) => (n >= 0 ? "+" : "") + fmt(n) + "%";
