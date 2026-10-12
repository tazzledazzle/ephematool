// Bazel-only Vitest config (used by //web:test). Bazel runs tests from a runfiles tree of
// symlinks; without preserveSymlinks Vite resolves test files to their real execroot paths,
// which lie outside the project root, and fails to load them. npm runs use vitest.config.ts.
import { mergeConfig } from "vitest/config";
import base from "./vitest.config";

export default mergeConfig(base, {
  resolve: { preserveSymlinks: true },
});
