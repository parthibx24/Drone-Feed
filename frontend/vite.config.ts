import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// drone-gateway (Go) serves the built app from web_dir and the API from /api.
// For `npm run dev`, run drone-gateway locally and proxy to it.
const backend = process.env.VITE_BACKEND ?? "http://localhost:8091";

export default defineConfig({
  base: "/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": { target: backend, ws: true },
    },
  },
});
