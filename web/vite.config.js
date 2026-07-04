import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Proxies API calls to the Go server so the SPA can use relative paths
// (/v1/..., /health) with no CORS setup.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});
