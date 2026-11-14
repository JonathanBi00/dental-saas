import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

export default defineConfig({
  // The router plugin must run before the React plugin.
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") },
  },
  server: {
    port: 5173,
    strictPort: true,
    // Same-origin in dev: the browser talks only to Vite, which forwards /api to the Go API.
    // This keeps the session cookie first-party and means no CORS (design D7).
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false, xfwd: true },
    },
  },
});
