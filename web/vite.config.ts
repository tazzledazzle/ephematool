import path from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

const env = process.env;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  define: {
    __BUILD_VERSION__: JSON.stringify(env.VITE_BUILD_VERSION || "dev"),
    __BUILD_COMMIT__: JSON.stringify(env.VITE_BUILD_COMMIT || "unknown"),
    __BUILD_TIME__: JSON.stringify(env.VITE_BUILD_TIME || new Date().toISOString()),
  },
  server: {
    // Local hacking only; the compose stack uses nginx as the same-origin front door.
    proxy: {
      "/api": "http://127.0.0.1:8081",
      "/otlp": {
        target: "http://127.0.0.1:4318",
        rewrite: (p) => p.replace(/^\/otlp/, ""),
      },
    },
  },
});
