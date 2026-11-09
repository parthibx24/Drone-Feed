import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const backend = process.env.VITE_BACKEND ?? "http://localhost:8091";

export default defineConfig({
  // FastAPI serves "/" from static/index.html and everything else under /static.
  base: "/static/",
  plugins: [react()],
  build: {
    outDir: "../static",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": backend,
      "/ws": { target: backend, ws: true },
    },
  },
});
