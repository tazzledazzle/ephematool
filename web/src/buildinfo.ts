// Dashboard build info is embedded at build time via Vite define (assumption A11).
export const dashboardBuild = {
  version: __BUILD_VERSION__,
  commit: __BUILD_COMMIT__,
  builtAt: __BUILD_TIME__,
} as const;
