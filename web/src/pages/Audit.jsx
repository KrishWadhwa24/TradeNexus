import React, { useEffect, useState } from "react";
import jsPDF from "jspdf";
import autoTable from "jspdf-autotable";
import { api, convLevel, convLabel } from "../api.js";
import { Icon } from "../icons.jsx";
import ChartModal from "../components/ChartModal.jsx";

export default function Audit({ isAdmin = false }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [tf, setTf] = useState("");
  const [source, setSource] = useState("");
  const [firing, setFiring] = useState(null); // signal id currently sending
  const [msg, setMsg] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [activeChart, setActiveChart] = useState(null); // { id, symbol, tf }
  const [showDownload, setShowDownload] = useState(false);
  const [downloading, setDownloading] = useState(false);

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

  // downloadPdf re-fetches with the currently applied filters (source, tf,
  // search) plus a day-window cutoff, then renders a PDF of exactly what's
  // shown on screen — so the download always matches the active filters.
  async function downloadPdf(days) {
    setDownloading(true);
    setMsg("");
    try {
      const q = new URLSearchParams({ limit: "2000" });
      if (tf) q.set("tf", tf);
      if (source) q.set("source", source);
      const r = await api.get("/v1/signals?" + q.toString());
      const cutoff = Date.now() - days * 24 * 60 * 60 * 1000;
      const filtered = (r.signals || [])
        .filter((s) => (s.symbol || "").toLowerCase().includes(searchQuery.toLowerCase()))
        .filter((s) => new Date(s.created_at).getTime() >= cutoff);

      const filterBits = [];
      if (source) filterBits.push(`source: ${source}`);
      if (tf) filterBits.push(`tf: ${tf}`);
      if (searchQuery) filterBits.push(`search: "${searchQuery}"`);

      const doc = new jsPDF({ orientation: "landscape" });
      doc.setFontSize(14);
      doc.text("TradeNexus - Signals Audit", 14, 15);
      doc.setFontSize(10);
      doc.text(
        `Last ${days} days${filterBits.length ? " - " + filterBits.join(", ") : ""} - generated ${new Date().toLocaleString()}`,
        14, 21
      );

      autoTable(doc, {
        startY: 27,
        head: [["Symbol", "Signal", "Source", "TF", "Conviction", "Price", "Scanner(s)", "Candle date", "Generated"]],
        body: filtered.map((s) => [
          s.symbol,
          s.direction,
          s.source,
          s.timeframe,
          convLevel(s.source, s.confidence) ? convLabel(s.source, s.confidence) : "-",
          s.price != null ? `Rs.${s.price.toFixed(2)}` : "-", // jsPDF's default font can't render ₹
          s.scanner_name,
          s.candle_date?.slice(0, 10) || "",
          new Date(s.created_at).toLocaleString(),
        ]),
        styles: { fontSize: 8 },
        headStyles: { fillColor: [59, 66, 92] },
      });

      doc.save(`signals_audit_${days}d.pdf`);
      setShowDownload(false);
    } catch (e) {
      setMsg(`Download failed: ${e.message}`);
    } finally {
      setDownloading(false);
    }
  }

  async function fire(sig) {
    setFiring(sig.id);
    setMsg("");
    try {
      const r = await api.post(`/v1/admin/dispatch/force?signal_id=${sig.id}`, {});
      const parts = [`sent to ${r.sent} recipient${r.sent === 1 ? "" : "s"}`];
      if (r.default_sent) parts.push("safety-net chat");
      setMsg(`🔥 ${sig.symbol} (${sig.timeframe}) re-fired — ${parts.join(" + ")}.`);
    } catch (e) {
      setMsg(`Failed to re-fire ${sig.symbol}: ${e.message}`);
    } finally {
      setFiring(null);
    }
  }

  return (
    <div>
      <div className="toolbar">
        <div className="section-title" style={{ margin: 0 }}>
          All signals (retained 100 days, then auto-removed)
        </div>
        <div className="row" style={{ flexWrap: "wrap" }}>
          {msg && <span className="msg" style={{ flex: "1 1 100%", marginBottom: 4 }}>{msg}</span>}
          <input
            className="btn-sm"
            type="text"
            placeholder="Search symbol"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            style={{ minWidth: 150 }}
          />
          <select value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">All sources</option>
            <option value="pine">Pine</option>
            <option value="weekly">Weekly</option>
            <option value="patterns">Patterns</option>
          </select>
          <select value={tf} onChange={(e) => setTf(e.target.value)}>
            <option value="">All timeframes</option>
            <option value="1D">1D</option>
            <option value="1W">1W</option>
            <option value="1M">1M</option>
          </select>
          <button className="btn-sm" onClick={load}>Refresh</button>
          <button className="btn-sm" onClick={() => setShowDownload(true)} disabled={!rows.length}>
            Download PDF
          </button>
        </div>
      </div>

      {isAdmin && (
        <div className="subtle" style={{ marginBottom: 12 }}>
          Admin: use the <b>Fire</b> button to re-send a signal's Telegram alert now — it bypasses the
          duplicate check and the 7-day send window.
        </div>
      )}

      {loading ? (
        <div className="spinner">Loading audit…</div>
      ) : err ? (
        <div className="err">{err}</div>
      ) : !rows.length ? (
        <div className="empty">No signals recorded.</div>
      ) : (
        <div className="panel" style={{ width: "100%" }}>
          <table className="audit-table" style={{ minWidth: 720 }}>
            <thead>
              <tr>
                <th>Symbol</th>
                <th style={{ textAlign: "center" }}>Signal</th>
                <th style={{ textAlign: "center" }}>Source</th>
                <th style={{ textAlign: "center" }}>TF</th>
                <th style={{ textAlign: "center" }}>Conviction</th>
                <th style={{ textAlign: "right" }}>Price</th>
                <th style={{ textAlign: "left" }}>Scanner(s)</th>
                <th>Candle date</th>
                <th>Generated</th>
                {isAdmin && <th>Alert</th>}
              </tr>
            </thead>
            <tbody>
              {rows.filter((s) => (s.symbol || "").toLowerCase().includes(searchQuery.toLowerCase())).map((s) => (
                <tr key={s.id}>
                  <td data-label="">
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      {s.symbol}
                      <button className="btn-sm btn-ghost" style={{ padding: '2px 6px', fontSize: '11px' }} onClick={() => setActiveChart({ id: s.instrument_id, symbol: s.symbol, tf: s.timeframe })}>Chart</button>
                    </div>
                  </td>
                  <td data-label="Signal" style={{ textAlign: "center" }}><span className={s.direction === "BUY" ? "tag tag-buy" : "tag tag-sell"}>{s.direction}</span></td>
                  <td data-label="Source" className="muted" style={{ textAlign: "center" }}>{s.source}</td>
                  <td data-label="Timeframe" style={{ textAlign: "center" }}>{s.timeframe}</td>
                  <td data-label="Conviction" style={{ textAlign: "center" }}>
                    {convLevel(s.source, s.confidence) ? (
                      <span className={"conv conv-" + convLevel(s.source, s.confidence)}>
                        {convLabel(s.source, s.confidence)}
                      </span>
                    ) : "—"}
                  </td>
                  <td data-label="Price" className="muted" style={{ textAlign: "right" }}>
                    {s.price != null ? `₹${s.price.toFixed(2)}` : "—"}
                  </td>
                  <td data-label="Scanner(s)" className="muted" style={{ textAlign: "left" }}>{s.scanner_name}</td>
                  <td data-label="Candle date" className="muted">{s.candle_date?.slice(0, 10)}</td>
                  <td data-label="Generated" className="muted" style={{ whiteSpace: "normal", minWidth: 140 }}>
                    {new Date(s.created_at).toLocaleString()}
                  </td>
                  {isAdmin && (
                    <td data-label="Alert">
                      <button
                        className="btn-sm fire-btn"
                        title="Re-send this alert on Telegram"
                        disabled={firing === s.id}
                        onClick={() => fire(s)}
                      >
                        <Icon.fire />
                        {firing === s.id ? "Firing…" : "Fire"}
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {activeChart && (
        <ChartModal
          instrumentId={activeChart.id}
          symbol={activeChart.symbol}
          tf={activeChart.tf}
          onClose={() => setActiveChart(null)}
        />
      )}

      {showDownload && (
        <div className="chart-modal-backdrop" onClick={() => !downloading && setShowDownload(false)}>
          <div className="chart-modal" style={{ width: "min(100%, 420px)" }} onClick={(e) => e.stopPropagation()}>
            <div className="chart-modal-header">
              <h3>Download PDF</h3>
              <button className="btn-sm btn-ghost" onClick={() => setShowDownload(false)} disabled={downloading}>Close</button>
            </div>
            <div style={{ padding: "0 22px 22px" }}>
              <p className="subtle" style={{ marginTop: 16 }}>
                Downloads with the filters currently applied
                {source || tf || searchQuery ? (
                  <> ({[source && `source: ${source}`, tf && `tf: ${tf}`, searchQuery && `search: "${searchQuery}"`].filter(Boolean).join(", ")})</>
                ) : " (none — all signals)"}. How many days back?
              </p>
              <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
                {[7, 14, 30, 100].map((d) => (
                  <button key={d} className="btn-sm" disabled={downloading} onClick={() => downloadPdf(d)}>
                    {downloading ? "Generating…" : `${d} days`}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
