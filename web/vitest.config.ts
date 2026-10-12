import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "src") } },
  define: {
    __BUILD_VERSION__: JSON.stringify("test"),
    __BUILD_COMMIT__: JSON.stringify("test"),
    __BUILD_TIME__: JSON.stringify("1970-01-01T00:00:00.000Z"),
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    exclude: ["e2e/**", "node_modules/**"],
  },
});
