import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev rejim: vmsd 8081 (API + WS). Build — statik; vmsd uni `web_dir`
// dan beradi (Go embed YO'Q — REJA §4.1).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8081", ws: true, changeOrigin: true },
    },
  },
  build: { outDir: "dist", sourcemap: true, emptyOutDir: true },
});
