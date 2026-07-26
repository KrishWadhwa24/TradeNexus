import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { createChart, CandlestickSeries, createSeriesMarkers } from "lightweight-charts";
import { api } from "../api";

const TF_LABELS = { "1D": "Daily", "1W": "Weekly", "1M": "Monthly" };

export default function ChartModal({ instrumentId, symbol, tf = "1D", onClose }) {
  const chartContainerRef = useRef(null);
  const chartRef = useRef(null);
  const seriesRef = useRef(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!loading && chartRef.current && chartContainerRef.current) {
      chartRef.current.applyOptions({
        width: chartContainerRef.current.clientWidth,
        height: chartContainerRef.current.clientHeight,
      });
      chartRef.current.timeScale().fitContent();
    }
  }, [loading]);

  useEffect(() => {
    if (!chartContainerRef.current) return;

    // Determine current theme to match chart styles
    const isLight = document.documentElement.getAttribute("data-theme") === "light";
    const bg = isLight ? "#ffffff" : "#0d0f13";
    const text = isLight ? "#12141a" : "#e8eaed";
    const grid = isLight ? "#eef0f3" : "#15181e";

    const chart = createChart(chartContainerRef.current, {
      layout: {
        background: { type: 'solid', color: bg },
        textColor: text,
      },
      grid: {
        vertLines: { color: grid },
        horzLines: { color: grid },
      },
      timeScale: {
        timeVisible: true,
        borderColor: grid,
      },
      rightPriceScale: {
        borderColor: grid,
      },
      crosshair: {
        mode: 1,
      },
    });

    const series = chart.addSeries(CandlestickSeries, {
      upColor: '#3ecf8e',
      downColor: '#f0616d',
      borderVisible: false,
      wickUpColor: '#3ecf8e',
      wickDownColor: '#f0616d',
    });

    chartRef.current = chart;
    seriesRef.current = series;

    const handleResize = () => {
      if (chartContainerRef.current) {
        chart.applyOptions({ width: chartContainerRef.current.clientWidth });
      }
    };
    window.addEventListener("resize", handleResize);

    const loadData = async () => {
      try {
        setLoading(true);
        // 1. Fetch candles
        const rCandles = await api.get(`/v1/instruments/${instrumentId}/candles?tf=${tf}&limit=300`);
        if (!rCandles.candles || rCandles.candles.length === 0) {
          setError("No candle data available.");
          setLoading(false);
          return;
        }

        // Daily candles key on `time`; weekly/monthly aggregates key on `period_start`.
        const candleData = rCandles.candles.map((c) => ({
          time: (c.time || c.period_start).split("T")[0],
          open: c.open,
          high: c.high,
          low: c.low,
          close: c.close,
        })).sort((a, b) => a.time.localeCompare(b.time));
        
        const uniqueCandles = [];
        const seen = new Set();
        for (const c of candleData) {
          if (!seen.has(c.time)) {
            uniqueCandles.push(c);
            seen.add(c.time);
          }
        }
        
        series.setData(uniqueCandles);

        // 2. Fetch signals
        const rSignals = await api.get(`/v1/signals?instrument_id=${instrumentId}&tf=${tf}&limit=100`);
        const signals = rSignals.signals || [];

        const markers = [];
        signals.forEach(sig => {
          if (!sig.candle_date) return;
          const dateStr = sig.candle_date.split("T")[0];
          
          if (sig.direction === "BUY") {
             markers.push({
               time: dateStr,
               position: 'belowBar',
               color: '#3ecf8e',
               shape: 'arrowUp',
               text: sig.source === 'pine' ? 'Pine' : 'Wkly'
             });
          } else if (sig.direction === "SELL") {
             markers.push({
               time: dateStr,
               position: 'aboveBar',
               color: '#f0616d',
               shape: 'arrowDown',
               text: sig.source === 'pine' ? 'Pine' : 'Wkly'
             });
          }
        });

        markers.sort((a, b) => a.time.localeCompare(b.time));
        createSeriesMarkers(series, markers);
        
        chart.timeScale().fitContent();
        setLoading(false);
      } catch (e) {
        setError(e.message);
        setLoading(false);
      }
    };

    loadData();

    return () => {
      window.removeEventListener("resize", handleResize);
      chart.remove();
    };
  }, [instrumentId, tf]);

  return createPortal(
    <div className="chart-modal-backdrop" onClick={onClose}>
      <div className="chart-modal" onClick={(e) => e.stopPropagation()}>
        <div className="chart-modal-header">
          <h3>{symbol} — {TF_LABELS[tf] || tf} Chart</h3>
          <button className="btn-sm btn-ghost" onClick={onClose}>Close</button>
        </div>

        {loading && <div className="spinner">Loading chart data...</div>}
        {error && <div className="err" style={{ padding: "0 22px 22px" }}>{error}</div>}

        <div
          ref={chartContainerRef}
          style={{ width: "100%", height: "460px", display: loading || error ? "none" : "block" }}
        />
      </div>
    </div>,
    document.body
  );
}
