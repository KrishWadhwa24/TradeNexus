import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "autoUpdate",
      // No runtimeCaching rules — this is a live trading platform, so /v1
      // API calls and the live-prices WebSocket must always hit the
      // network fresh. The service worker only precaches the built app
      // shell (JS/CSS/HTML), never live data, so "installed as an app"
      // never means "showing you yesterday's prices."
      manifest: {
        name: "TradeNexus",
        short_name: "TradeNexus",
        description: "NSE/BSE stock scanners, IPO GMP tracking, promoter buying analysis, and bulk/block deal alerts.",
        start_url: "/",
        display: "standalone",
        background_color: "#0b0d11",
        theme_color: "#0b0d11",
        icons: [
          { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
          { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png" },
          { src: "/icons/icon-512-maskable.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
    }),
  ],
  server: {
    host: true,
    allowedHosts: true,
    port: 5173,
    proxy: {
      "/v1": {
        target: "http://localhost:8080",
        ws: true,
      },
      "/health": "http://localhost:8080",
    },
  },
  // Same proxy as `server` above — `vite preview` serves the real
  // production build (with the service worker active), and needs this to
  // test against the real backend the same way `vite dev` does.
  preview: {
    proxy: {
      "/v1": {
        target: "http://localhost:8080",
        ws: true,
      },
      "/health": "http://localhost:8080",
    },
  },
});
