import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    allowedHosts: true,
    port: 5173,
    proxy: {
      "/v1": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});