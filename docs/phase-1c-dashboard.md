# Phase 1c · Dashboard: React SPA, browser telemetry, login and status pages

**Objective:** build the web dashboard: a React 19 + Vite SPA served by nginx (the same-origin front door for the SPA, the API and browser OTLP), with browser OpenTelemetry (page load, fetch, logs, metrics) joined into the **same trace** as the API and worker, GitHub sign-in with a sliding session, an access-denied page, a user menu with theme switching, and the telemetry status page (ten services, grouped, with trace-aware error states). Includes Vitest unit tests and Playwright e2e specs, plus the Bazel wiring.

**Design references:** `.planning/phases/01-telemetry-spine-github-login/01-UI-SPEC.md` (design system, copy, states), `01-CONTEXT.md` (D-03, D-05..D-08), `01-RESEARCH.md` (Pattern 4, assumption A9).
**Requirements covered:** OBS-01 (dashboard emits telemetry), AUTH-01.
**Prerequisite:** Phase 1a and 1b acceptance checklists complete. `api/openapi.json` exists.
**Produces (later phases rely on these exact names):**

| Module | Public API used later |
|---|---|
| `@/api/client` | `api` (typed `openapi-fetch` client), `setPageLoadContext`, `pageLoadDone` |
| `@/telemetry` | `initTelemetry`, `uiLogger`, `uiMetrics` |
| `@/components/AppShell` | the authenticated layout every later page renders inside |
| `@/features/auth/useMe` | current user + role for gating UI |
| `@/components/ui/*` | shadcn primitives (Phase 2 adds more) |
| nginx `/api/` and `/otlp/` | same-origin API and browser OTLP |

---

## 1. Directory structure after this phase

```
web/
├── BUILD.bazel  Dockerfile  Dockerfile.dockerignore  .dockerignore
├── package.json  tsconfig*.json  vite.config.ts  vitest*.ts  playwright.config.ts
├── components.json  pnpm-workspace.yaml  index.html
├── e2e/{helpers,auth.spec,status.spec,telemetry.spec,grafana.spec}.ts
└── src/
    ├── main.tsx  App.tsx  router.tsx  index.css  buildinfo.ts  vite-env.d.ts
    ├── api/{client.ts,client.test.ts,schema.d.ts}        # schema.d.ts is generated
    ├── gen/conventions.gen.ts                            # generated
    ├── telemetry/{index,tracing,metrics,logs,pageLoad,resource}.ts  telemetry.test.ts
    ├── lib/{utils.ts,theme.ts,theme.test.ts,ThemeProvider.tsx}
    ├── components/{AppShell,PageHeading,RoleBadge,UserMenu}.tsx
    ├── components/ui/{avatar,badge,button,card,dropdown-menu,skeleton,table,tooltip}.tsx
    └── features/
        ├── auth/{LoginPage,DeniedPage}.tsx  useMe.ts  auth.test.tsx
        └── status/{StatusPage,ServiceTable,HealthBadge,BuildCard,SignedInCard,StatusError}.tsx
            relativeTime.ts  status.test.tsx
deploy/nginx/default.conf
```

```bash
mkdir -p web/e2e web/src/{api,gen,telemetry,lib,components/ui,features/auth,features/status}
mkdir -p deploy/nginx
```

---

## 2. Project configuration

`package.json` pins exact versions (no `^`), so `npm install` is reproducible. Create it, then run `npm install` inside `web/` once; that writes `package-lock.json`, which Bazel (`rules_js`) turns into `pnpm-lock.yaml` on the first build. **npm stays the package manager; Bazel only reads the lock.**

`web/package.json`

```json
{
  "name": "ephematool-web",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest",
    "gen": "openapi-typescript ../api/openapi.json -o src/api/schema.d.ts",
    "e2e": "playwright test"
  },
  "dependencies": {
    "@opentelemetry/api": "1.9.1",
    "@opentelemetry/api-logs": "0.223.0",
    "@opentelemetry/core": "2.12.0",
    "@opentelemetry/exporter-logs-otlp-http": "0.223.0",
    "@opentelemetry/exporter-metrics-otlp-http": "0.223.0",
    "@opentelemetry/exporter-trace-otlp-http": "0.223.0",
    "@opentelemetry/instrumentation": "0.223.0",
    "@opentelemetry/instrumentation-document-load": "0.68.0",
    "@opentelemetry/instrumentation-fetch": "0.223.0",
    "@opentelemetry/resources": "2.12.0",
    "@opentelemetry/sdk-logs": "0.223.0",
    "@opentelemetry/sdk-metrics": "2.12.0",
    "@opentelemetry/sdk-trace-base": "2.12.0",
    "@opentelemetry/sdk-trace-web": "2.12.0",
    "@tanstack/react-query": "5.104.2",
    "@tanstack/react-router": "1.170.42",
    "class-variance-authority": "0.7.1",
    "clsx": "2.1.1",
    "lucide-react": "1.55.0",
    "openapi-fetch": "0.17.0",
    "radix-ui": "1.8.0",
    "react": "19.3.0",
    "react-dom": "19.3.0",
    "tailwind-merge": "3.7.0",
    "tw-animate-css": "1.4.0"
  },
  "devDependencies": {
    "@playwright/test": "1.64.0",
    "@tailwindcss/vite": "4.3.3",
    "@testing-library/react": "16.3.3",
    "@types/node": "24.19.2",
    "@types/react": "19.3.0",
    "@types/react-dom": "19.3.0",
    "@vitejs/plugin-react": "6.1.2",
    "jsdom": "30.1.2",
    "openapi-typescript": "7.13.0",
    "tailwindcss": "4.3.3",
    "typescript": "5.9.3",
    "vite": "8.3.4",
    "vitest": "5.0.3"
  }
}
```
`web/tsconfig.json`

```json
{
  "files": [],
  "references": [{ "path": "./tsconfig.app.json" }, { "path": "./tsconfig.node.json" }],
  "compilerOptions": {
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  }
}
```
`web/tsconfig.app.json`

```json
{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.app.tsbuildinfo",
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "verbatimModuleSyntax": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] }
  },
  "include": ["src"]
}
```
`web/tsconfig.node.json`

```json
{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.node.tsbuildinfo",
    "target": "ES2023",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "verbatimModuleSyntax": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true,
    "types": ["node"]
  },
  "include": ["vite.config.ts", "vitest.config.ts", "vitest.bazel.config.ts", "playwright.config.ts", "e2e"]
}
```
`web/vite.config.ts`

```typescript
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
```
`vitest.config.ts` is for `npm test`; `vitest.bazel.config.ts` exists only because Bazel runs tests from a symlink tree (without `preserveSymlinks`, Vite resolves files outside the project root, and without `dedupe`, React is loaded twice and hooks fail with "Invalid hook call").

`web/vitest.config.ts`

```typescript
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
```
`web/vitest.bazel.config.ts`

```typescript
// Bazel-only Vitest config (used by //web:test). Bazel runs tests from a runfiles tree of
// symlinks; without preserveSymlinks Vite resolves test files to their real execroot paths,
// which lie outside the project root, and fails to load them. npm runs use vitest.config.ts.
// dedupe keeps a single copy of React: with preserveSymlinks, sources and react-dom otherwise
// resolve "react" through different symlink paths ("Invalid hook call" / null dispatcher).
import { mergeConfig } from "vitest/config";
import base from "./vitest.config";

export default mergeConfig(base, {
  resolve: { preserveSymlinks: true, dedupe: ["react", "react-dom"] },
});
```
`web/playwright.config.ts`

```typescript
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "e2e",
  outputDir: "test-results",
  retries: 0,
  use: {
    baseURL: "http://localhost:8080",
    trace: "on-first-retry",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
```
`web/components.json`

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "new-york",
  "rsc": false,
  "tsx": true,
  "tailwind": {
    "config": "",
    "css": "src/index.css",
    "baseColor": "neutral",
    "cssVariables": true,
    "prefix": ""
  },
  "iconLibrary": "lucide",
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils",
    "ui": "@/components/ui",
    "lib": "@/lib",
    "hooks": "@/hooks"
  }
}
```
`web/pnpm-workspace.yaml`

```yaml
# Read only by Bazel (rules_js). npm stays the package manager: web/package.json and
# web/package-lock.json are the dependency source of truth; rules_js derives
# web/pnpm-lock.yaml from package-lock.json.
#
# No dependency needs its install scripts (fsevents ships prebuilt binaries).
allowBuilds:
  fsevents: false
```
`web/index.html`

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Ephematool</title>
    <script>
      // Apply the stored theme before first paint to avoid a flash of the wrong theme.
      (function () {
        try {
          var c = localStorage.getItem("ephematool-theme");
          if (c !== "light" && c !== "dark" && c !== "system") c = "system";
          var dark =
            c === "dark" ||
            (c === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
          document.documentElement.classList.toggle("dark", dark);
        } catch (e) {}
      })();
    </script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```
```bash
cd web && npm install && cd ..
```

---

## 3. Generated inputs

Two files in the SPA are generated, never hand-edited:

| File | Source | Command |
|---|---|---|
| `src/gen/conventions.gen.ts` | `conventions/telemetry.json` | `bazel run //internal/conventions:gen` (Phase 1a wrote it already via `go run`) |
| `src/api/schema.d.ts` | `api/openapi.json` | `bazel run //web:gen` (after section 9), or `cd web && npm run gen` |

The typed client below fails to compile until `schema.d.ts` exists. If you want to type along before wiring Bazel, run `cd web && npm run gen` now.

`web/src/gen/conventions.gen.ts`

```typescript
// Code generated by genconventions. DO NOT EDIT.

export const K8S_LABEL_PREFIX = "ephematool.io/" as const;
export const OTEL_ATTR_PREFIX = "ephematool.env." as const;

export const LABELS = {
  envId: "ephematool.io/env-id",
  expiresAt: "ephematool.io/expires-at",
  managed: "ephematool.io/managed",
  owner: "ephematool.io/owner",
  template: "ephematool.io/template",
} as const;

export const ATTRS = {
  envId: "ephematool.env.id",
  expiresAt: "ephematool.env.expires-at",
  owner: "ephematool.env.owner",
  template: "ephematool.env.template",
} as const;
```
---

## 4. Browser telemetry

The page-load span is the trick that joins the browser into one trace. `startPageLoad()` creates a root span; requests made while the first screen renders are its children (the Fetch instrumentation injects `traceparent`, nginx forwards it, the API continues the trace, and the API enqueues the worker task with the same context). The screen that finishes loading calls `pageLoadDone()`; a 10 s auto-end is the backstop (assumption A9).

OTLP goes to `/otlp/v1/{traces,logs,metrics}` on the **same origin**, so no CORS is needed. nginx forwards those three paths (POST only, size-capped, rate-limited, cookies stripped) to the Collector.

`web/src/telemetry/resource.ts`

```typescript
import { resourceFromAttributes } from "@opentelemetry/resources";
import { dashboardBuild } from "@/buildinfo";

// Shared by traces, logs, and metrics so all three signals carry one identity.
export function dashboardResource() {
  return resourceFromAttributes({
    "service.name": "ephematool-dashboard",
    "service.namespace": "ephematool",
    "service.version": dashboardBuild.version,
  });
}
```
`web/src/telemetry/tracing.ts`

```typescript
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import { W3CTraceContextPropagator } from "@opentelemetry/core";
import { BatchSpanProcessor, type SpanProcessor } from "@opentelemetry/sdk-trace-base";
import { StackContextManager, WebTracerProvider } from "@opentelemetry/sdk-trace-web";
import { dashboardResource } from "./resource";

export function defaultSpanProcessors(): SpanProcessor[] {
  return [new BatchSpanProcessor(new OTLPTraceExporter({ url: "/otlp/v1/traces" }))];
}

// Test seam: lets the unit test pass an in-memory exporter. SDK 2.x takes span
// processors in the constructor (the old per-provider add method is gone).
export function createTracerProvider(
  spanProcessors: SpanProcessor[] = defaultSpanProcessors(),
): WebTracerProvider {
  const provider = new WebTracerProvider({
    resource: dashboardResource(),
    spanProcessors,
  });
  // Registered explicitly (A9) so synchronous context.with works without Zone.js
  // and does not depend on the library default.
  provider.register({
    contextManager: new StackContextManager(),
    propagator: new W3CTraceContextPropagator(),
  });
  return provider;
}
```
`web/src/telemetry/metrics.ts`

```typescript
import {
  metrics,
  type Attributes,
  type Counter,
  type Histogram,
  type Meter,
} from "@opentelemetry/api";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { MeterProvider, PeriodicExportingMetricReader } from "@opentelemetry/sdk-metrics";
import { dashboardResource } from "./resource";

// Metric attributes stay bounded: only `route` (a route template) and `outcome`.
// Never put user login or full URLs on metrics.
let meter: Meter = metrics.getMeter("ephematool-dashboard");

let pageViews: Counter | undefined;
let pageLoadDuration: Histogram | undefined;
let apiErrors: Counter | undefined;

function createInstruments(m: Meter) {
  pageViews = m.createCounter("ephematool.ui.page_views", {
    description: "Dashboard page loads",
  });
  pageLoadDuration = m.createHistogram("ephematool.ui.page_load.duration", {
    description: "Time from bootstrap to first status render",
    unit: "s",
  });
  apiErrors = m.createCounter("ephematool.ui.api_errors", {
    description: "Failed API requests seen by the dashboard",
  });
}
createInstruments(meter);

// Thin wrappers so importers can hold `uiMetrics` before initTelemetry() runs.
export const uiMetrics = {
  pageViews: { add: (v: number, a?: Attributes) => pageViews?.add(v, a) } as Counter,
  pageLoadDuration: {
    record: (v: number, a?: Attributes) => pageLoadDuration?.record(v, a),
  } as Histogram,
  apiErrors: { add: (v: number, a?: Attributes) => apiErrors?.add(v, a) } as Counter,
};

export function setupMetrics(): MeterProvider {
  const provider = new MeterProvider({
    resource: dashboardResource(),
    readers: [
      new PeriodicExportingMetricReader({
        exporter: new OTLPMetricExporter({ url: "/otlp/v1/metrics" }),
        exportIntervalMillis: 15000,
      }),
    ],
  });
  meter = provider.getMeter("ephematool-dashboard");
  createInstruments(meter);
  return provider;
}
```
`web/src/telemetry/logs.ts`

```typescript
import { logs } from "@opentelemetry/api-logs";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-http";
import { BatchLogRecordProcessor, LoggerProvider } from "@opentelemetry/sdk-logs";
import { dashboardResource } from "./resource";

// Delegates to the real provider once setupLogs() registers it.
export const uiLogger = logs.getLogger("ephematool-dashboard");

export function setupLogs(): LoggerProvider {
  const provider = new LoggerProvider({
    resource: dashboardResource(),
    processors: [new BatchLogRecordProcessor({
        exporter: new OTLPLogExporter({ url: "/otlp/v1/logs" }),
      })],
  });
  logs.setGlobalLoggerProvider(provider);
  return provider;
}
```
`web/src/telemetry/pageLoad.ts`

```typescript
import { context, trace, type Context } from "@opentelemetry/api";
import { SeverityNumber } from "@opentelemetry/api-logs";
import { uiLogger } from "./logs";
import { uiMetrics } from "./metrics";

const AUTO_END_MS = 10_000;

// Route templates only (bounded cardinality); never the raw pathname.
function routeTemplate(): string {
  switch (window.location.pathname) {
    case "/":
      return "/";
    case "/login":
      return "/login";
    case "/denied":
      return "/denied";
    default:
      return "unknown";
  }
}

export function startPageLoad(): { context: Context; end(): void } {
  const tracer = trace.getTracer("ephematool-dashboard");
  const started = performance.now();
  const route = routeTemplate();
  const span = tracer.startSpan("ui.page_load", { attributes: { route } });
  const pageContext = trace.setSpan(context.active(), span);

  let ended = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const end = () => {
    if (ended) return;
    ended = true;
    if (timer !== undefined) clearTimeout(timer);
    const durationMs = performance.now() - started;
    uiMetrics.pageLoadDuration.record(durationMs / 1000, { route });
    uiMetrics.pageViews.add(1, { route });
    // Emit inside the page context so the log record carries trace_id/span_id.
    context.with(pageContext, () => {
      uiLogger.emit({
        severityNumber: SeverityNumber.INFO,
        severityText: "INFO",
        body: "ui.page_load",
        attributes: { route, duration_ms: Math.round(durationMs) },
      });
    });
    span.end();
  };

  timer = setTimeout(end, AUTO_END_MS);
  return { context: pageContext, end };
}

// Runs window.fetch inside the page-load context. window.fetch is looked up at
// call time so FetchInstrumentation's patch (applied at init) is used.
export function tracedFetch(getCtx: () => Context | undefined): typeof fetch {
  return (input, init) =>
    context.with(getCtx() ?? context.active(), () => window.fetch(input, init));
}
```
`web/src/telemetry/index.ts`

```typescript
import { DocumentLoadInstrumentation } from "@opentelemetry/instrumentation-document-load";
import { FetchInstrumentation } from "@opentelemetry/instrumentation-fetch";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { setupLogs } from "./logs";
import { setupMetrics } from "./metrics";
import { createTracerProvider } from "./tracing";

export { uiLogger } from "./logs";
export { uiMetrics } from "./metrics";

let initialized = false;

// Idempotent; must run before React renders.
export function initTelemetry(): void {
  if (initialized) return;
  initialized = true;

  const tracerProvider = createTracerProvider();
  const loggerProvider = setupLogs();
  const meterProvider = setupMetrics();

  registerInstrumentations({
    tracerProvider,
    instrumentations: [
      new FetchInstrumentation({
        // Same origin: traceparent is injected without CORS allowlisting.
        propagateTraceHeaderCorsUrls: [],
        // Do not trace the exporters' own calls.
        ignoreUrls: [/\/otlp\//],
        clearTimingResources: true,
      }),
      // Produces a separate documentLoad trace (RESEARCH Pattern 4).
      new DocumentLoadInstrumentation(),
    ],
  });

  window.addEventListener("pagehide", () => {
    void tracerProvider.forceFlush();
    void loggerProvider.forceFlush();
    void meterProvider.forceFlush();
  });
}
```
`web/src/buildinfo.ts`

```typescript
// Dashboard build info is embedded at build time via Vite define (assumption A11).
export const dashboardBuild = {
  version: __BUILD_VERSION__,
  commit: __BUILD_COMMIT__,
  builtAt: __BUILD_TIME__,
} as const;
```
`web/src/vite-env.d.ts`

```typescript
/// <reference types="vite/client" />

declare const __BUILD_VERSION__: string;
declare const __BUILD_COMMIT__: string;
declare const __BUILD_TIME__: string;
```
`web/src/telemetry/telemetry.test.ts`

```typescript
import { context, propagation, trace } from "@opentelemetry/api";
import { InMemorySpanExporter, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { startPageLoad, tracedFetch } from "./pageLoad";
import { createTracerProvider } from "./tracing";

const exporter = new InMemorySpanExporter();

beforeAll(() => {
  createTracerProvider([new SimpleSpanProcessor(exporter)]);
});

afterEach(() => {
  exporter.reset();
  vi.restoreAllMocks();
});

describe("A9: ui.page_load is the parent of fetches without Zone.js", () => {
  it("runs the fetch with the ui.page_load span active and injects its trace id", async () => {
    const page = startPageLoad();
    const pageSpan = trace.getSpan(page.context)!;
    const pageCtx = pageSpan.spanContext();

    let activeSpanId: string | undefined;
    const carrier: Record<string, string> = {};
    window.fetch = vi.fn(async () => {
      activeSpanId = trace.getActiveSpan()?.spanContext().spanId;
      propagation.inject(context.active(), carrier);
      return new Response("ok");
    }) as unknown as typeof fetch;

    await tracedFetch(() => page.context)("/api/status");
    page.end();

    expect(activeSpanId).toBe(pageCtx.spanId);
    expect(carrier.traceparent).toMatch(new RegExp(`^00-${pageCtx.traceId}-[0-9a-f]{16}-01$`));
  });

  it("falls back to the active context when no page context is available", async () => {
    window.fetch = vi.fn(async () => new Response("ok")) as unknown as typeof fetch;
    await expect(tracedFetch(() => undefined)("/api/status")).resolves.toBeInstanceOf(Response);
    expect(window.fetch).toHaveBeenCalledTimes(1);
  });

  it("end() is idempotent and exports exactly one ui.page_load span", () => {
    const page = startPageLoad();
    page.end();
    page.end();
    const spans = exporter.getFinishedSpans().filter((s) => s.name === "ui.page_load");
    expect(spans).toHaveLength(1);
  });

  it("tags spans with service.name and service.namespace", () => {
    const page = startPageLoad();
    page.end();
    const [span] = exporter.getFinishedSpans().filter((s) => s.name === "ui.page_load");
    expect(span.resource.attributes["service.name"]).toBe("ephematool-dashboard");
    expect(span.resource.attributes["service.namespace"]).toBe("ephematool");
  });
});
```
---

## 5. API client

`openapi-fetch` gives a fully typed client from `schema.d.ts`. Requests run inside the page-load trace context through `tracedFetch`, and `ApiError` carries the `X-Trace-Id` of the failed response so error states can show the trace id.

`web/src/api/client.ts`

```typescript
import type { Context } from "@opentelemetry/api";
import { SeverityNumber } from "@opentelemetry/api-logs";
import createClient, { type Middleware } from "openapi-fetch";
import { uiLogger, uiMetrics } from "@/telemetry";
import { tracedFetch } from "@/telemetry/pageLoad";
import type { paths } from "./schema";

// The page-load context parents the requests made while the first screen renders.
// It is cleared once the page load ends so later polls get their own root span.
let pageLoadCtx: Context | undefined;
let pageLoadEnd: (() => void) | undefined;

export function setPageLoadContext(ctx: Context | undefined, end?: () => void): void {
  pageLoadCtx = ctx;
  pageLoadEnd = ctx ? end : undefined;
}

// Ends the ui.page_load span and detaches the context. Safe to call repeatedly.
export function pageLoadDone(): void {
  const end = pageLoadEnd;
  setPageLoadContext(undefined);
  end?.();
}

const HEX32 = /^[0-9a-f]{32}$/;

function traceIdFrom(headers: Headers): string | null {
  const explicit = headers.get("x-trace-id")?.trim().toLowerCase();
  if (explicit && HEX32.test(explicit)) return explicit;
  const parts = headers.get("traceparent")?.trim().toLowerCase().split("-");
  if (parts && parts.length === 4 && HEX32.test(parts[1])) return parts[1];
  return null;
}

export class ApiError extends Error {
  readonly status: number;
  readonly traceId: string | null;

  constructor(message: string, status: number, traceId: string | null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.traceId = traceId;
  }

  static fromResponse(response: Response, message?: string): ApiError {
    return new ApiError(
      message ?? `Request failed with status ${response.status}`,
      response.status,
      traceIdFrom(response.headers),
    );
  }
}

// Navigation seam: a plain object so tests can spy on it.
export const session = {
  expire(): void {
    window.location.assign("/login?reason=expired");
  },
};

const handleResponse: Middleware = {
  onResponse({ response, schemaPath }) {
    if (response.ok) return;
    // /api/me answers 401 for anonymous visitors by design; the route guard handles it.
    const expectedAnonymous = response.status === 401 && schemaPath === "/api/me";
    if (!expectedAnonymous) {
      const outcome = response.status >= 500 ? "server_error" : "client_error";
      // schemaPath is the OpenAPI template, so the label stays bounded.
      uiMetrics.apiErrors.add(1, { route: schemaPath, outcome });
      uiLogger.emit({
        severityNumber: SeverityNumber.WARN,
        severityText: "WARN",
        body: "ui.api_error",
        attributes: {
          route: schemaPath,
          status: response.status,
          trace_id: traceIdFrom(response.headers) ?? "",
        },
      });
    }
    if (response.status === 401 && schemaPath !== "/api/me") {
      session.expire();
    }
  },
};

export function createApi(baseUrl = "/") {
  const client = createClient<paths>({
    baseUrl,
    fetch: tracedFetch(() => pageLoadCtx),
  });
  client.use(handleResponse);
  return client;
}

export const api = createApi();
```
`web/src/api/client.test.ts`

```typescript
import { context, trace } from "@opentelemetry/api";
import { InMemorySpanExporter, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { createTracerProvider } from "@/telemetry/tracing";
import { ApiError, createApi, session, setPageLoadContext } from "./client";

const exporter = new InMemorySpanExporter();
const TRACE = "0af7651916cd43dd8448eb211c80319c";
const ORIGIN = "http://localhost:8080";

beforeAll(() => {
  createTracerProvider([new SimpleSpanProcessor(exporter)]);
});

afterEach(() => {
  exporter.reset();
  setPageLoadContext(undefined);
  vi.restoreAllMocks();
});

function mockFetch(res: () => Response): ReturnType<typeof vi.fn> {
  const fn = vi.fn(async () => res());
  window.fetch = fn as unknown as typeof fetch;
  return fn;
}

describe("ApiError trace id", () => {
  it("prefers the X-Trace-Id header", () => {
    const res = new Response("boom", { status: 500, headers: { "X-Trace-Id": TRACE } });
    const err = ApiError.fromResponse(res);
    expect(err.status).toBe(500);
    expect(err.traceId).toBe(TRACE);
  });

  it("falls back to the traceparent header", () => {
    const res = new Response("boom", {
      status: 502,
      headers: { traceparent: `00-${TRACE}-b7ad6b7169203331-01` },
    });
    expect(ApiError.fromResponse(res).traceId).toBe(TRACE);
  });

  it("is null when neither header is present", () => {
    expect(ApiError.fromResponse(new Response("boom", { status: 500 })).traceId).toBeNull();
  });
});

describe("401 handling", () => {
  it("redirects to /login?reason=expired on a 401 from /api/status", async () => {
    const expire = vi.spyOn(session, "expire").mockImplementation(() => {});
    mockFetch(() => new Response("{}", { status: 401, headers: { "content-type": "application/json" } }));
    const res = await createApi(ORIGIN).GET("/api/status");
    expect(res.response.status).toBe(401);
    expect(expire).toHaveBeenCalledTimes(1);
  });

  it("does not redirect on a 401 from /api/me (the route guard handles it)", async () => {
    const expire = vi.spyOn(session, "expire").mockImplementation(() => {});
    mockFetch(() => new Response("{}", { status: 401, headers: { "content-type": "application/json" } }));
    const res = await createApi(ORIGIN).GET("/api/me");
    expect(res.response.status).toBe(401);
    expect(expire).not.toHaveBeenCalled();
  });
});

describe("page-load context", () => {
  it("runs fetch inside the context set by setPageLoadContext, then under the active context", async () => {
    const tracer = trace.getTracer("test");
    const pageSpan = tracer.startSpan("ui.page_load");
    const pageCtx = trace.setSpan(context.active(), pageSpan);

    let activeSpanId: string | undefined;
    mockFetch(() => {
      activeSpanId = trace.getActiveSpan()?.spanContext().spanId;
      return new Response("{}", { status: 200, headers: { "content-type": "application/json" } });
    });
    const api = createApi(ORIGIN);

    setPageLoadContext(pageCtx);
    await api.GET("/api/auth/config");
    expect(activeSpanId).toBe(pageSpan.spanContext().spanId);

    setPageLoadContext(undefined);
    await api.GET("/api/auth/config");
    expect(activeSpanId).toBeUndefined();
    pageSpan.end();
  });
});
```
---

## 6. Design system and shared UI

Styling is Tailwind v4 with shadcn/ui "new-york" primitives (Radix under the hood). The theme is defined once in `index.css` as CSS variables, with light, dark and system modes; `index.html` applies the stored theme **before first paint** to avoid a flash.

The primitives under `components/ui/` come from the shadcn CLI. You can regenerate them with:

```bash
cd web && npx shadcn@latest add avatar badge button card dropdown-menu skeleton table tooltip
```

but the versions below are what the project uses; paste them if you want an identical build.

`web/src/index.css`

```css
@import "tailwindcss";
@import "tw-animate-css";

@custom-variant dark (&:is(.dark *));

/* Colors per 01-UI-SPEC. Components use these variables, never hard-coded hex. */
:root {
  --background: #FFFFFF;
  --foreground: #18181B;
  --card: #F4F4F5;
  --card-foreground: #18181B;
  --popover: #FFFFFF;
  --popover-foreground: #18181B;
  --primary: #2563EB;
  --primary-foreground: #FFFFFF;
  --secondary: #F4F4F5;
  --secondary-foreground: #18181B;
  --muted: #F4F4F5;
  --muted-foreground: #52525B;
  --accent: #F4F4F5;
  --accent-foreground: #18181B;
  --destructive: #B91C1C;
  --warning: #B45309;
  --border: #E4E4E7;
  --input: #E4E4E7;
  --ring: #2563EB;
}

.dark {
  --background: #09090B;
  --foreground: #FAFAFA;
  --card: #18181B;
  --card-foreground: #FAFAFA;
  --popover: #18181B;
  --popover-foreground: #FAFAFA;
  --primary: #60A5FA;
  --primary-foreground: #09090B;
  --secondary: #18181B;
  --secondary-foreground: #FAFAFA;
  --muted: #18181B;
  --muted-foreground: #A1A1AA;
  --accent: #27272A;
  --accent-foreground: #FAFAFA;
  --destructive: #F87171;
  --warning: #FBBF24;
  --border: #27272A;
  --input: #27272A;
  --ring: #60A5FA;
}

@theme inline {
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-warning: var(--warning);
  --color-border: var(--border);
  --color-input: var(--input);
  --color-ring: var(--ring);
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 8px;
}

/* Typography: four sizes (12/14/20/28), two weights (400/600). */
@theme {
  --font-sans: Inter, ui-sans-serif, system-ui, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, monospace;

  --text-*: initial;
  --text-xs: 12px;
  --text-xs--line-height: 1.5;
  --text-sm: 14px;
  --text-sm--line-height: 1.5;
  --text-base: 14px;
  --text-base--line-height: 1.5;
  --text-xl: 20px;
  --text-xl--line-height: 1.2;
  --text-2xl: 28px;
  --text-2xl--line-height: 1.2;

  --font-weight-*: initial;
  --font-weight-normal: 400;
  --font-weight-semibold: 600;
}

@layer base {
  * {
    border-color: var(--border);
  }
  html {
    font-size: 14px;
  }
  body {
    background: var(--background);
    color: var(--foreground);
    font-family: var(--font-sans);
    font-size: 14px;
    line-height: 1.5;
    font-weight: 400;
  }
  /* Focus ring: 2px with 2px offset in the accent color of the active theme. */
  :focus-visible {
    outline: 2px solid var(--ring);
    outline-offset: 2px;
  }
}

/* Skeleton tint is static under reduced motion. */
@media (prefers-reduced-motion: reduce) {
  .animate-pulse {
    animation: none;
  }
}
```
`web/src/lib/utils.ts`

```typescript
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```
`web/src/lib/theme.ts`

```typescript
export type ThemeChoice = "light" | "dark" | "system";

export const THEME_STORAGE_KEY = "ephematool-theme";

const CHOICES: readonly ThemeChoice[] = ["light", "dark", "system"];

// Only the three known values are accepted; anything else falls back to "system" (T-01-12).
export function readThemeChoice(): ThemeChoice {
  try {
    const v = window.localStorage.getItem(THEME_STORAGE_KEY);
    return CHOICES.includes(v as ThemeChoice) ? (v as ThemeChoice) : "system";
  } catch {
    return "system";
  }
}

export function resolveTheme(choice: ThemeChoice, prefersDark: boolean): "light" | "dark" {
  if (choice === "system") return prefersDark ? "dark" : "light";
  return choice;
}

export function applyTheme(choice: ThemeChoice): void {
  const prefersDark =
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.classList.toggle("dark", resolveTheme(choice, prefersDark) === "dark");
}
```
`web/src/lib/ThemeProvider.tsx`

```tsx
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { applyTheme, readThemeChoice, THEME_STORAGE_KEY, type ThemeChoice } from "@/lib/theme";

type ThemeContextValue = { choice: ThemeChoice; setChoice: (c: ThemeChoice) => void };

const ThemeContext = createContext<ThemeContextValue | null>(null);

// Temporarily disables CSS transitions so switching themes never animates (UI-SPEC).
function withoutTransitions(fn: () => void): void {
  const style = document.createElement("style");
  style.appendChild(document.createTextNode("*,*::before,*::after{transition:none!important}"));
  document.head.appendChild(style);
  fn();
  // Force a style flush, then remove the override on the next frame.
  void window.getComputedStyle(document.body).opacity;
  window.requestAnimationFrame(() => style.remove());
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [choice, setChoiceState] = useState<ThemeChoice>(readThemeChoice);

  const setChoice = useCallback((c: ThemeChoice) => {
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, c);
    } catch {
      // Storage can be unavailable (private mode); the choice still applies for this page.
    }
    setChoiceState(c);
  }, []);

  useEffect(() => {
    withoutTransitions(() => applyTheme(choice));
    if (choice !== "system" || typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => withoutTransitions(() => applyTheme("system"));
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [choice]);

  const value = useMemo(() => ({ choice, setChoice }), [choice, setChoice]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
```
`web/src/lib/theme.test.ts`

```typescript
import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme, readThemeChoice, resolveTheme, THEME_STORAGE_KEY } from "./theme";

describe("theme", () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove("dark");
  });

  it("resolves system against prefers-color-scheme", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
  });

  it("defaults to system when nothing or garbage is stored", () => {
    expect(readThemeChoice()).toBe("system");
    window.localStorage.setItem(THEME_STORAGE_KEY, "purple");
    expect(readThemeChoice()).toBe("system");
  });

  it("returns stored valid values", () => {
    for (const v of ["light", "dark", "system"] as const) {
      window.localStorage.setItem(THEME_STORAGE_KEY, v);
      expect(readThemeChoice()).toBe(v);
    }
  });

  it("applyTheme toggles the dark class", () => {
    applyTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
});
```
`web/src/components/ui/avatar.tsx`

```tsx
import * as React from "react"
import { cn } from "@/lib/utils"
import { Avatar as AvatarPrimitive } from "radix-ui"

function Avatar({
  className,
  size = "default",
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Root> & {
  size?: "default" | "sm" | "lg"
}) {
  return (
    <AvatarPrimitive.Root
      data-slot="avatar"
      data-size={size}
      className={cn(
        "group/avatar relative flex size-8 shrink-0 overflow-hidden rounded-full select-none data-[size=lg]:size-10 data-[size=sm]:size-6",
        className
      )}
      {...props}
    />
  )
}

function AvatarImage({
  className,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Image>) {
  return (
    <AvatarPrimitive.Image
      data-slot="avatar-image"
      className={cn("aspect-square size-full", className)}
      {...props}
    />
  )
}

function AvatarFallback({
  className,
  ...props
}: React.ComponentProps<typeof AvatarPrimitive.Fallback>) {
  return (
    <AvatarPrimitive.Fallback
      data-slot="avatar-fallback"
      className={cn(
        "flex size-full items-center justify-center rounded-full bg-muted text-sm text-muted-foreground group-data-[size=sm]/avatar:text-xs",
        className
      )}
      {...props}
    />
  )
}

function AvatarBadge({ className, ...props }: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="avatar-badge"
      className={cn(
        "absolute right-0 bottom-0 z-10 inline-flex items-center justify-center rounded-full bg-primary text-primary-foreground ring-2 ring-background select-none",
        "group-data-[size=sm]/avatar:size-2 group-data-[size=sm]/avatar:[&>svg]:hidden",
        "group-data-[size=default]/avatar:size-2.5 group-data-[size=default]/avatar:[&>svg]:size-2",
        "group-data-[size=lg]/avatar:size-3 group-data-[size=lg]/avatar:[&>svg]:size-2",
        className
      )}
      {...props}
    />
  )
}

function AvatarGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="avatar-group"
      className={cn(
        "group/avatar-group flex -space-x-2 *:data-[slot=avatar]:ring-2 *:data-[slot=avatar]:ring-background",
        className
      )}
      {...props}
    />
  )
}

function AvatarGroupCount({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="avatar-group-count"
      className={cn(
        "relative flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-sm text-muted-foreground ring-2 ring-background group-has-data-[size=lg]/avatar-group:size-10 group-has-data-[size=sm]/avatar-group:size-6 [&>svg]:size-4 group-has-data-[size=lg]/avatar-group:[&>svg]:size-5 group-has-data-[size=sm]/avatar-group:[&>svg]:size-3",
        className
      )}
      {...props}
    />
  )
}

export {
  Avatar,
  AvatarImage,
  AvatarFallback,
  AvatarBadge,
  AvatarGroup,
  AvatarGroupCount,
}
```
`web/src/components/ui/badge.tsx`

```tsx
import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"
import { Slot } from "radix-ui"

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-full border border-transparent px-2 py-0.5 text-xs font-semibold whitespace-nowrap transition-[color,box-shadow] aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a&]:hover:bg-primary/90",
        secondary:
          "bg-secondary text-secondary-foreground [a&]:hover:bg-secondary/90",
        destructive:
          "bg-destructive text-white focus-visible:ring-destructive/20 dark:bg-destructive/60 dark:focus-visible:ring-destructive/40 [a&]:hover:bg-destructive/90",
        outline:
          "border-border text-foreground [a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
        ghost: "[a&]:hover:bg-accent [a&]:hover:text-accent-foreground",
        link: "text-primary underline-offset-4 [a&]:hover:underline",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"span"> &
  VariantProps<typeof badgeVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "span"

  return (
    <Comp
      data-slot="badge"
      data-variant={variant}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
}

export { Badge, badgeVariants }
```
`web/src/components/ui/button.tsx`

```tsx
import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"
import { Slot } from "radix-ui"

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md text-sm font-semibold whitespace-nowrap transition-all outline-none disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive:
          "bg-destructive text-white hover:bg-destructive/90 focus-visible:ring-destructive/20 dark:bg-destructive/60 dark:focus-visible:ring-destructive/40",
        outline:
          "border bg-background shadow-xs hover:bg-accent hover:text-accent-foreground dark:border-input dark:bg-input/30 dark:hover:bg-input/50",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        ghost:
          "hover:bg-accent hover:text-accent-foreground dark:hover:bg-accent/50",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-11 px-4 py-2 has-[>svg]:px-3",
        xs: "h-6 gap-1 rounded-md px-2 text-xs has-[>svg]:px-1.5 [&_svg:not([class*='size-'])]:size-3",
        sm: "h-8 gap-1.5 rounded-md px-3 has-[>svg]:px-2.5",
        lg: "h-10 rounded-md px-6 has-[>svg]:px-4",
        icon: "size-11",
        "icon-xs": "size-6 rounded-md [&_svg:not([class*='size-'])]:size-3",
        "icon-sm": "size-8",
        "icon-lg": "size-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

function Button({
  className,
  variant = "default",
  size = "default",
  asChild = false,
  ...props
}: React.ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : "button"

  return (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
```
`web/src/components/ui/card.tsx`

```tsx
import * as React from "react"
import { cn } from "@/lib/utils"

function Card({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card"
      className={cn(
        "flex flex-col gap-6 rounded-xl border bg-card py-6 text-card-foreground shadow-sm",
        className
      )}
      {...props}
    />
  )
}

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6",
        className
      )}
      {...props}
    />
  )
}

function CardTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-title"
      className={cn("leading-none font-semibold", className)}
      {...props}
    />
  )
}

function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("text-sm text-muted-foreground", className)}
      {...props}
    />
  )
}

function CardAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        "col-start-2 row-span-2 row-start-1 self-start justify-self-end",
        className
      )}
      {...props}
    />
  )
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("px-6", className)}
      {...props}
    />
  )
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn("flex items-center px-6 [.border-t]:pt-6", className)}
      {...props}
    />
  )
}

export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardAction,
  CardDescription,
  CardContent,
}
```
`web/src/components/ui/dropdown-menu.tsx`

```tsx
"use client"

import * as React from "react"
import { cn } from "@/lib/utils"
import { CheckIcon, ChevronRightIcon } from "lucide-react"
import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui"

function DropdownMenu({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Root>) {
  return <DropdownMenuPrimitive.Root data-slot="dropdown-menu" {...props} />
}

function DropdownMenuPortal({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Portal>) {
  return (
    <DropdownMenuPrimitive.Portal data-slot="dropdown-menu-portal" {...props} />
  )
}

function DropdownMenuTrigger({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Trigger>) {
  return (
    <DropdownMenuPrimitive.Trigger
      data-slot="dropdown-menu-trigger"
      {...props}
    />
  )
}

function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Content>) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        data-slot="dropdown-menu-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 max-h-(--radix-dropdown-menu-content-available-height) min-w-[8rem] origin-(--radix-dropdown-menu-content-transform-origin) overflow-x-hidden overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
          className
        )}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  )
}

function DropdownMenuGroup({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Group>) {
  return (
    <DropdownMenuPrimitive.Group data-slot="dropdown-menu-group" {...props} />
  )
}

function DropdownMenuItem({
  className,
  inset,
  variant = "default",
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Item> & {
  inset?: boolean
  variant?: "default" | "destructive"
}) {
  return (
    <DropdownMenuPrimitive.Item
      data-slot="dropdown-menu-item"
      data-inset={inset}
      data-variant={variant}
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[inset]:pl-8 data-[variant=destructive]:text-destructive data-[variant=destructive]:focus:bg-destructive/10 data-[variant=destructive]:focus:text-destructive dark:data-[variant=destructive]:focus:bg-destructive/20 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground data-[variant=destructive]:*:[svg]:text-destructive!",
        className
      )}
      {...props}
    />
  )
}

function DropdownMenuCheckboxItem({
  className,
  children,
  checked,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.CheckboxItem>) {
  return (
    <DropdownMenuPrimitive.CheckboxItem
      data-slot="dropdown-menu-checkbox-item"
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm py-1.5 pr-2 pl-8 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      checked={checked}
      {...props}
    >
      <span className="pointer-events-none absolute left-2 flex size-4 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <CheckIcon className="size-4" />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.CheckboxItem>
  )
}

function DropdownMenuRadioGroup({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioGroup>) {
  return (
    <DropdownMenuPrimitive.RadioGroup
      data-slot="dropdown-menu-radio-group"
      {...props}
    />
  )
}

function DropdownMenuRadioItem({
  className,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.RadioItem>) {
  return (
    <DropdownMenuPrimitive.RadioItem
      data-slot="dropdown-menu-radio-item"
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm py-1.5 pr-2 pl-8 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
        className
      )}
      {...props}
    >
      <span className="pointer-events-none absolute left-2 flex size-4 items-center justify-center">
        <DropdownMenuPrimitive.ItemIndicator>
          <CheckIcon className="size-4" />
        </DropdownMenuPrimitive.ItemIndicator>
      </span>
      {children}
    </DropdownMenuPrimitive.RadioItem>
  )
}

function DropdownMenuLabel({
  className,
  inset,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Label> & {
  inset?: boolean
}) {
  return (
    <DropdownMenuPrimitive.Label
      data-slot="dropdown-menu-label"
      data-inset={inset}
      className={cn(
        "px-2 py-1.5 text-sm font-semibold data-[inset]:pl-8",
        className
      )}
      {...props}
    />
  )
}

function DropdownMenuSeparator({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Separator>) {
  return (
    <DropdownMenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-border", className)}
      {...props}
    />
  )
}

function DropdownMenuShortcut({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      data-slot="dropdown-menu-shortcut"
      className={cn(
        "ml-auto text-xs tracking-widest text-muted-foreground",
        className
      )}
      {...props}
    />
  )
}

function DropdownMenuSub({
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.Sub>) {
  return <DropdownMenuPrimitive.Sub data-slot="dropdown-menu-sub" {...props} />
}

function DropdownMenuSubTrigger({
  className,
  inset,
  children,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubTrigger> & {
  inset?: boolean
}) {
  return (
    <DropdownMenuPrimitive.SubTrigger
      data-slot="dropdown-menu-sub-trigger"
      data-inset={inset}
      className={cn(
        "flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden select-none focus:bg-accent focus:text-accent-foreground data-[inset]:pl-8 data-[state=open]:bg-accent data-[state=open]:text-accent-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 [&_svg:not([class*='text-'])]:text-muted-foreground",
        className
      )}
      {...props}
    >
      {children}
      <ChevronRightIcon className="ml-auto size-4" />
    </DropdownMenuPrimitive.SubTrigger>
  )
}

function DropdownMenuSubContent({
  className,
  ...props
}: React.ComponentProps<typeof DropdownMenuPrimitive.SubContent>) {
  return (
    <DropdownMenuPrimitive.SubContent
      data-slot="dropdown-menu-sub-content"
      className={cn(
        "z-50 min-w-[8rem] origin-(--radix-dropdown-menu-content-transform-origin) overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-lg data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95",
        className
      )}
      {...props}
    />
  )
}

export {
  DropdownMenu,
  DropdownMenuPortal,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
}
```
`web/src/components/ui/skeleton.tsx`

```tsx
import { cn } from "@/lib/utils"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-accent", className)}
      {...props}
    />
  )
}

export { Skeleton }
```
`web/src/components/ui/table.tsx`

```tsx
import * as React from "react"
import { cn } from "@/lib/utils"

function Table({ className, ...props }: React.ComponentProps<"table">) {
  return (
    <div
      data-slot="table-container"
      className="relative w-full overflow-x-auto"
    >
      <table
        data-slot="table"
        className={cn("w-full caption-bottom text-sm", className)}
        {...props}
      />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  return (
    <thead
      data-slot="table-header"
      className={cn("[&_tr]:border-b", className)}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  return (
    <tbody
      data-slot="table-body"
      className={cn("[&_tr:last-child]:border-0", className)}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<"tfoot">) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn(
        "border-t bg-muted/50 font-semibold [&>tr]:last:border-b-0",
        className
      )}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<"tr">) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        "border-b transition-colors hover:bg-muted/50 has-aria-expanded:bg-muted/50 data-[state=selected]:bg-muted",
        className
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        "h-10 px-2 text-left align-middle font-semibold whitespace-nowrap text-foreground [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<"td">) {
  return (
    <td
      data-slot="table-cell"
      className={cn(
        "p-2 align-middle whitespace-nowrap [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className
      )}
      {...props}
    />
  )
}

function TableCaption({
  className,
  ...props
}: React.ComponentProps<"caption">) {
  return (
    <caption
      data-slot="table-caption"
      className={cn("mt-4 text-sm text-muted-foreground", className)}
      {...props}
    />
  )
}

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
```
`web/src/components/ui/tooltip.tsx`

```tsx
"use client"

import * as React from "react"
import { cn } from "@/lib/utils"
import { Tooltip as TooltipPrimitive } from "radix-ui"

function TooltipProvider({
  delayDuration = 0,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Provider>) {
  return (
    <TooltipPrimitive.Provider
      data-slot="tooltip-provider"
      delayDuration={delayDuration}
      {...props}
    />
  )
}

function Tooltip({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Root>) {
  return <TooltipPrimitive.Root data-slot="tooltip" {...props} />
}

function TooltipTrigger({
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Trigger>) {
  return <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
}

function TooltipContent({
  className,
  sideOffset = 0,
  children,
  ...props
}: React.ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        data-slot="tooltip-content"
        sideOffset={sideOffset}
        className={cn(
          "z-50 w-fit origin-(--radix-tooltip-content-transform-origin) animate-in rounded-md bg-foreground px-3 py-1.5 text-xs text-balance text-background fade-in-0 zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95",
          className
        )}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="z-50 size-2.5 translate-y-[calc(-50%_-_2px)] rotate-45 rounded-[2px] bg-foreground fill-foreground" />
      </TooltipPrimitive.Content>
    </TooltipPrimitive.Portal>
  )
}

export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider }
```
---

## 7. App shell, routing and auth screens

Routing is TanStack Router. `/login` and `/denied` are public; everything else renders inside `AppShell`, which calls `/api/me` and navigates to `/login` on a 401 (no flash of content). It renders a loading skeleton while identity loads, a retry card with the trace id if `/api/me` fails for any other reason, and only mounts the page `<Outlet />` once identity is known, so an anonymous visit never fires a 401 that would look like an expired session.

`web/src/components/PageHeading.tsx`

```tsx
import { useEffect, useRef, type ReactNode } from "react";
import { cn } from "@/lib/utils";

type Role = "display" | "heading";

// Page title. Takes focus on mount so route changes are announced and keyboard
// users start at the top of the new page (tabindex -1, no visible outline needed).
export function PageHeading({
  children,
  role = "heading",
  className,
}: {
  children: ReactNode;
  role?: Role;
  className?: string;
}) {
  const ref = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    ref.current?.focus({ preventScroll: true });
  }, []);
  return (
    <h1
      ref={ref}
      tabIndex={-1}
      className={cn(
        "leading-[1.2] font-semibold outline-none",
        role === "display" ? "text-[28px]" : "text-xl",
        className,
      )}
    >
      {children}
    </h1>
  );
}
```
`web/src/components/RoleBadge.tsx`

```tsx
import { Badge } from "@/components/ui/badge";

export function RoleBadge({ role }: { role: "admin" | "viewer" }) {
  return (
    <Badge variant="secondary" className="text-xs font-semibold">
      {role === "admin" ? "Admin" : "Viewer"}
    </Badge>
  );
}
```
`web/src/components/UserMenu.tsx`

```tsx
import { useState } from "react";
import { RoleBadge } from "@/components/RoleBadge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useTheme } from "@/lib/ThemeProvider";
import type { ThemeChoice } from "@/lib/theme";

type Props = {
  login: string;
  avatarUrl: string;
  role: "admin" | "viewer";
  onSignOut: () => Promise<void>;
};

const THEME_OPTIONS: ReadonlyArray<{ value: ThemeChoice; label: string }> = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
];

// Sign-out (POST /api/auth/logout, clear the query cache, navigate) lives in the caller so this
// component stays free of router and query-client context.
export function UserMenu({ login, avatarUrl, role, onSignOut }: Props) {
  const { choice, setChoice } = useTheme();
  const [signingOut, setSigningOut] = useState(false);

  const signOut = async () => {
    setSigningOut(true);
    try {
      await onSignOut();
    } finally {
      setSigningOut(false);
    }
  };

  return (
    <TooltipProvider>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="size-8 rounded-full p-0"
                aria-label={`Open user menu for ${login}`}
              >
                <Avatar className="size-8">
                  <AvatarImage src={avatarUrl} alt="" />
                  <AvatarFallback>{login.slice(0, 1).toUpperCase()}</AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>Account menu</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end" className="min-w-56">
          <div className="flex items-center gap-2 px-2 py-1.5 text-sm">
            <span className="font-semibold">{login}</span>
            <RoleBadge role={role} />
          </div>
          <DropdownMenuSeparator />
          <DropdownMenuLabel>Theme</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={choice} onValueChange={(v) => setChoice(v as ThemeChoice)}>
            {THEME_OPTIONS.map((o) => (
              // Keep the menu open so the effect of the change is visible.
              <DropdownMenuRadioItem key={o.value} value={o.value} onSelect={(e) => e.preventDefault()}>
                {o.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={signingOut} onSelect={() => void signOut()}>
            Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </TooltipProvider>
  );
}
```
`web/src/components/AppShell.tsx`

```tsx
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, Outlet, useNavigate } from "@tanstack/react-router";
import { api, ApiError } from "@/api/client";
import { UserMenu } from "@/components/UserMenu";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/useMe";

// Rendered from an array so later phases add items without touching the markup.
const NAV_ITEMS = [{ to: "/", label: "Telemetry" }] as const;

export function AppShell() {
  const me = useMe();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [signOutFailed, setSignOutFailed] = useState(false);

  const signOut = async () => {
    setSignOutFailed(false);
    try {
      const { response } = await api.POST("/api/auth/logout");
      if (!response.ok) throw ApiError.fromResponse(response);
      queryClient.clear();
      await navigate({ to: "/login", replace: true });
    } catch {
      setSignOutFailed(true);
    }
  };

  // Not signed in: leave the shell (no flash of content, no extra toast).
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="fixed inset-x-0 top-0 z-10 flex h-14 items-center gap-6 border-b bg-secondary px-6">
        <span className="text-xl leading-[1.2] font-semibold">Ephematool</span>
        <nav aria-label="Primary">
          <ul className="flex items-center gap-4">
            {NAV_ITEMS.map((item) => (
              <li key={item.to}>
                <Link
                  to={item.to}
                  className="inline-flex h-14 items-center border-b-2 border-transparent text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:border-primary aria-[current=page]:text-primary"
                  activeOptions={{ exact: true }}
                >
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className="ml-auto flex items-center gap-3">
          {me.isPending && <Skeleton className="size-8 rounded-full" />}
          {me.data && (
            <UserMenu
              login={me.data.login}
              avatarUrl={me.data.avatarUrl}
              role={me.data.role}
              onSignOut={signOut}
            />
          )}
        </div>
      </header>
      <main className="mx-auto w-full max-w-[1024px] px-6 pt-[104px] pb-12">
        {signOutFailed && (
          <p role="alert" className="mb-4 text-sm text-destructive">
            Sign out failed. Try again.
          </p>
        )}
        {me.isPending && (
          <div className="flex flex-col gap-6" aria-busy="true">
            <Skeleton className="h-7 w-64" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
        {me.isError && !(me.error instanceof ApiError && me.error.status === 401) && (
          <div role="alert" className="flex flex-col gap-3 text-sm">
            <p>Couldn&apos;t load your account.</p>
            {me.error instanceof ApiError && me.error.traceId && (
              <p>
                Trace <code className="font-mono">{me.error.traceId}</code>
              </p>
            )}
            <div>
              <Button variant="outline" onClick={() => void me.refetch()}>
                Retry
              </Button>
            </div>
          </div>
        )}
        {/* Status loads only once identity is known, so an anonymous visit never
            fires a 401 that would look like an expired session. */}
        {me.isSuccess && <Outlet />}
      </main>
    </div>
  );
}
```
`web/src/features/auth/useMe.ts`

```typescript
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/api/client";

// The shell calls GET /api/me once per mount. No retries: a 401 means "not signed in".
export function useMe() {
  return useQuery({
    queryKey: ["me"],
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/me");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });
}
```
`web/src/features/auth/LoginPage.tsx`

```tsx
import { useEffect } from "react";
import { Navigate, useSearch } from "@tanstack/react-router";
import { pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { useMe } from "./useMe";

function GithubMark() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="currentColor" className="size-5">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

export function LoginPage() {
  const search = useSearch({ strict: false }) as { reason?: string; next?: string };
  const me = useMe();

  // This page has no status request, so the page load ends once identity is known.
  useEffect(() => {
    if (me.isSuccess || me.isError) pageLoadDone();
  }, [me.isSuccess, me.isError]);

  // Already signed in: go straight to the app.
  if (me.isSuccess) return <Navigate to="/" replace />;

  const start = () => {
    // Full-page navigation into the OAuth flow; never fetch (the provider redirects).
    window.location.assign("/api/auth/github/login?next=" + encodeURIComponent(search.next ?? "/"));
  };

  return (
    <main className="min-h-screen bg-background px-4 pt-16">
      <div className="mx-auto flex w-full max-w-[400px] flex-col gap-4 rounded-xl border bg-card p-8 text-card-foreground">
        <p className="text-xs leading-normal font-semibold text-muted-foreground">Ephematool</p>
        <PageHeading role="display">Sign in to Ephematool</PageHeading>
        {search.reason === "expired" && (
          <p className="text-sm leading-normal">You&apos;ve been signed out. Sign in to continue.</p>
        )}
        {search.reason === "failed" && (
          <p role="alert" className="text-sm leading-normal text-destructive">
            GitHub sign-in didn&apos;t complete. Try again.
          </p>
        )}
        <p className="text-sm leading-normal text-muted-foreground">
          Use your GitHub account to continue. Access is limited to approved organizations.
        </p>
        <Button className="min-h-11 w-full" onClick={start}>
          <GithubMark />
          Continue with GitHub
        </Button>
      </div>
    </main>
  );
}
```
`web/src/features/auth/DeniedPage.tsx`

```tsx
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { api, ApiError, pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";

// Public page: it must not depend on a session. `login` comes from the URL and is
// rendered as React text only; the required orgs come from the API, never the URL.
export function DeniedPage() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as { login?: string };
  const cfg = useQuery({
    queryKey: ["auth-config"],
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/auth/config");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });

  useEffect(() => {
    if (cfg.isSuccess || cfg.isError) pageLoadDone();
  }, [cfg.isSuccess, cfg.isError]);

  const required = [...(cfg.data?.allowedOrgs ?? []), ...(cfg.data?.allowedTeams ?? [])];

  return (
    <main className="min-h-screen bg-background px-4 pt-16">
      <div className="mx-auto flex w-full max-w-[400px] flex-col gap-4 rounded-xl border bg-card p-8 text-card-foreground">
        <PageHeading role="display">Access denied</PageHeading>
        <p className="text-sm leading-normal">
          Your GitHub account ({search.login ?? "unknown"}) is not a member of{" "}
          {required.length === 0
            ? "an approved organization"
            : required.map((r, i) => (
                <span key={r}>
                  {i > 0 && " or "}
                  <code className="font-mono">{r}</code>
                </span>
              ))}
          . Ask an owner of that organization to add you, then sign in again.
        </p>
        <Button variant="secondary" className="min-h-11 w-full" onClick={() => void navigate({ to: "/login" })}>
          Try a different account
        </Button>
      </div>
    </main>
  );
}
```
`web/src/features/auth/auth.test.tsx`

```tsx
import { createContext, useContext } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserMenu } from "@/components/UserMenu";
import { ThemeProvider } from "@/lib/ThemeProvider";
import { THEME_STORAGE_KEY } from "@/lib/theme";
import { DeniedPage } from "./DeniedPage";
import { LoginPage } from "./LoginPage";

const nav = vi.hoisted(() => ({ search: {} as Record<string, string>, navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useSearch: () => nav.search,
  useNavigate: () => nav.navigate,
  Navigate: () => null,
}));

vi.mock("./useMe", () => ({
  useMe: () => ({ isSuccess: false, isError: true }),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    pageLoadDone: vi.fn(),
    api: {
      GET: vi.fn(async () => ({
        data: { githubConfigured: true, allowedOrgs: ["ephematool-e2e"], allowedTeams: ["acme/sre"] },
        response: new Response(null, { status: 200 }),
      })),
      POST: vi.fn(async () => ({ response: new Response(null, { status: 204 }) })),
    },
  };
});

// Radix tooltips load a second copy of React under Bazel's symlinked node_modules
// ("Invalid hook call"), so unit tests stub the tooltip primitives (same as status.test.tsx).
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/avatar", () => ({
  Avatar: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
  AvatarImage: () => null,
  AvatarFallback: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
}));

// Radix avatar and menus also load their own React copy under Bazel ("Invalid hook call"), so
// the avatar and dropdown primitives are stubbed with a minimal always-open equivalent. The real menu (open, keyboard,
// stays open on select) is exercised by the Playwright auth spec.
vi.mock("@/components/ui/dropdown-menu", () => {
  type Group = { value: string; onValueChange: (v: string) => void };
  const GroupCtx = createContext<Group | null>(null);
  const Pass = ({ children }: { children?: React.ReactNode }) => <>{children}</>;
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger: Pass,
    DropdownMenuContent: ({ children }: { children?: React.ReactNode }) => <div role="menu">{children}</div>,
    DropdownMenuLabel: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuSeparator: () => <hr />,
    DropdownMenuItem: ({ children, onSelect }: { children?: React.ReactNode; onSelect?: () => void }) => (
      <button role="menuitem" onClick={() => onSelect?.()}>
        {children}
      </button>
    ),
    DropdownMenuRadioGroup: ({ children, value, onValueChange }: Group & { children?: React.ReactNode }) => (
      <GroupCtx.Provider value={{ value, onValueChange }}>
        <div role="group">{children}</div>
      </GroupCtx.Provider>
    ),
    DropdownMenuRadioItem: ({ children, value }: { children?: React.ReactNode; value: string }) => {
      const g = useContext(GroupCtx)!;
      return (
        <button role="menuitemradio" aria-checked={g.value === value} onClick={() => g.onValueChange(value)}>
          {children}
        </button>
      );
    },
  };
});

beforeEach(() => {
  nav.search = {};
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function withClient(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("LoginPage reasons", () => {
  it("shows the expired copy", () => {
    nav.search = { reason: "expired" };
    render(<LoginPage />);
    expect(screen.getByText("You've been signed out. Sign in to continue.")).toBeTruthy();
  });

  it("shows the failed copy", () => {
    nav.search = { reason: "failed" };
    render(<LoginPage />);
    expect(screen.getByText("GitHub sign-in didn't complete. Try again.")).toBeTruthy();
  });

  it("shows nothing for an unknown reason and never echoes it", () => {
    nav.search = { reason: "<script>alert(1)</script>" };
    const { container } = render(<LoginPage />);
    expect(container.innerHTML).not.toContain("script");
    expect(screen.queryByText(/signed out/)).toBeNull();
    expect(screen.queryByText(/didn't complete/)).toBeNull();
  });
});

describe("DeniedPage", () => {
  it("renders the login as literal text and joins orgs with or", async () => {
    nav.search = { login: "<b>mallory</b>" };
    const { container } = render(withClient(<DeniedPage />));
    expect(screen.getByText(/<b>mallory<\/b>/)).toBeTruthy();
    expect(container.querySelector("b")).toBeNull();
    await waitFor(() => expect(screen.getByText("ephematool-e2e")).toBeTruthy());
    expect(screen.getByText("acme/sre").tagName).toBe("CODE");
    expect(container.textContent).toContain("ephematool-e2e or acme/sre");
  });

  it("focuses the heading and returns to /login", async () => {
    nav.search = { login: "octo-outsider" };
    render(withClient(<DeniedPage />));
    expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Access denied" }));
    fireEvent.click(screen.getByRole("button", { name: "Try a different account" }));
    expect(nav.navigate).toHaveBeenCalledWith({ to: "/login" });
  });
});

describe("UserMenu theme", () => {
  it("persists the chosen theme", async () => {
    render(
      withClient(
        <ThemeProvider>
          <UserMenu login="octo-admin" avatarUrl="" role="admin" onSignOut={async () => {}} />
        </ThemeProvider>,
      ),
    );
    expect(screen.getByRole("button", { name: "Open user menu for octo-admin" })).toBeTruthy();
    expect(screen.getByRole("menuitemradio", { name: "System" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Dark" }));
    await waitFor(() => expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark"));
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(screen.getByRole("menuitemradio", { name: "Dark" }).getAttribute("aria-checked")).toBe("true");
  });
});
```
`web/src/router.tsx`

```tsx
import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "@/components/AppShell";
import { DeniedPage } from "@/features/auth/DeniedPage";
import { LoginPage } from "@/features/auth/LoginPage";
import { StatusPage } from "@/features/status/StatusPage";

const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

const rootRoute = createRootRoute({ component: Outlet });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (s: Record<string, unknown>): { reason?: string; next?: string } => ({
    reason: str(s.reason),
    next: str(s.next),
  }),
  component: LoginPage,
});

const deniedRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/denied",
  validateSearch: (s: Record<string, unknown>): { reason?: string; login?: string } => ({
    reason: str(s.reason),
    login: str(s.login),
  }),
  component: DeniedPage,
});

// Pathless layout route: everything inside needs a signed-in user.
const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: AppShell,
});

const statusRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: StatusPage,
});

const routeTree = rootRoute.addChildren([loginRoute, deniedRoute, shellRoute.addChildren([statusRoute])]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
```
`web/src/App.tsx`

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "./router";

// UI-SPEC: no silent retries and no focus-driven refetches; the user retries explicitly.
export const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
```
`web/src/main.tsx`

```tsx
import { StrictMode } from "react";
import ReactDOM from "react-dom/client";
import { setPageLoadContext } from "@/api/client";
import { initTelemetry } from "@/telemetry";
import { startPageLoad } from "@/telemetry/pageLoad";
import { ThemeProvider } from "@/lib/ThemeProvider";
import App from "./App";
import "./index.css";

// Telemetry must be live before React renders so the page load is traced.
initTelemetry();

// Requests made while the first screen renders are children of this span. The
// screen that finishes loading calls pageLoadDone(); the 10s auto-end is the backstop.
const pageLoad = startPageLoad();
setPageLoadContext(pageLoad.context, pageLoad.end);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <App />
    </ThemeProvider>
  </StrictMode>,
);
```
---

## 8. Telemetry status page

Ten fixed rows in four groups (platform, data, pipeline, backends). Each row shows a health badge, a "last signal" relative time, and a detail string. If `/api/status` itself fails, `StatusError` shows the HTTP status and a "view trace" link built from `X-Trace-Id` so the failure is debuggable from the failing page.

`web/src/features/status/relativeTime.ts`

```typescript
// Compact relative time for the status page: "12s ago", "3m ago", "2h ago", "4d ago".
// Absolute timestamps belong in a title attribute; this is only the glanceable form.
export function relativeTime(ts: string | Date | null | undefined, now: number = Date.now()): string {
  if (!ts) return "—";
  const then = ts instanceof Date ? ts.getTime() : Date.parse(ts);
  if (Number.isNaN(then)) return "—";
  const secs = Math.max(0, Math.round((now - then) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
```
`web/src/features/status/HealthBadge.tsx`

```tsx
import { AlertTriangle, CheckCircle2, HelpCircle, XCircle, type LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

export type Health = "healthy" | "degraded" | "down" | "unknown";

// Color is never the only signal: every state has an icon and a text label (UI-SPEC).
// Colors come from theme variables only; the accent color is never used on badges.
const STATES: Record<Health, { label: string; Icon: LucideIcon; tone: string }> = {
  healthy: { label: "Healthy", Icon: CheckCircle2, tone: "text-foreground" },
  degraded: { label: "Degraded", Icon: AlertTriangle, tone: "text-warning" },
  down: { label: "Down", Icon: XCircle, tone: "text-destructive" },
  unknown: { label: "Unknown", Icon: HelpCircle, tone: "text-muted-foreground" },
};

export function HealthBadge({ status }: { status: Health }) {
  const { label, Icon, tone } = STATES[status];
  return (
    <Badge variant="secondary" className={cn("text-xs font-semibold", tone)} data-health={status}>
      <Icon aria-hidden="true" />
      {label}
    </Badge>
  );
}
```
`web/src/features/status/ServiceTable.tsx`

```tsx
import { Fragment } from "react";
import type { components } from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { HealthBadge } from "./HealthBadge";
import { relativeTime } from "./relativeTime";

type Service = components["schemas"]["ServiceStatus"];

// Group order is the UI-SPEC contract; rows keep the API order inside each group.
const GROUPS: Array<{ id: Service["group"]; label: string }> = [
  { id: "platform", label: "Platform" },
  { id: "data", label: "Data" },
  { id: "pipeline", label: "Telemetry pipeline" },
  { id: "backends", label: "Observability backends" },
];

export function ServiceTable({
  services,
  now,
  onRetry,
}: {
  services: Service[];
  now: number;
  onRetry?: () => void;
}) {
  if (services.length === 0) {
    return (
      <div className="flex flex-col gap-2 py-6 text-sm">
        <p className="text-xl leading-[1.2] font-semibold">No service data yet</p>
        <p>
          The API has not received telemetry from any service. Check that the OTel Collector is
          running, then select Retry Loading Status.
        </p>
        {onRetry && (
          <div>
            <Button variant="outline" onClick={onRetry}>
              Retry Loading Status
            </Button>
          </div>
        )}
      </div>
    );
  }

  return (
    <Table>
      <TableHeader className="max-md:sr-only">
        <TableRow>
          <TableHead className="text-xs font-semibold">Service</TableHead>
          <TableHead className="text-xs font-semibold">Status</TableHead>
          <TableHead className="text-xs font-semibold">Last signal</TableHead>
        </TableRow>
      </TableHeader>
      {GROUPS.map((group) => {
        const rows = services.filter((s) => s.group === group.id);
        if (rows.length === 0) return null;
        return (
          <Fragment key={group.id}>
            <TableBody>
              <TableRow className="bg-secondary hover:bg-secondary">
                <TableHead
                  colSpan={3}
                  scope="colgroup"
                  data-group-header
                  className="text-xs font-semibold text-secondary-foreground"
                >
                  {group.label}
                </TableHead>
              </TableRow>
              {rows.map((s) => (
                <TableRow
                  key={s.id}
                  data-service-row
                  className="max-md:grid max-md:grid-cols-[1fr_auto] max-md:items-center"
                  title={s.detail || undefined}
                >
                  <TableCell data-service-name className="text-sm font-semibold max-md:col-span-2">
                    {s.name}
                  </TableCell>
                  <TableCell>
                    <HealthBadge status={s.status} />
                  </TableCell>
                  <TableCell
                    data-last-signal
                    title={s.lastSignalAt ?? undefined}
                    className="font-mono text-sm text-muted-foreground max-md:text-right"
                  >
                    {relativeTime(s.lastSignalAt, now)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Fragment>
        );
      })}
    </Table>
  );
}
```
`web/src/features/status/BuildCard.tsx`

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime } from "./relativeTime";

export type Build = { version: string; commit: string; builtAt: string };

function BuildColumn({ title, build, now }: { title: string; build: Build; now: number }) {
  const parsed = Date.parse(build.builtAt);
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-xs leading-normal font-semibold text-muted-foreground">{title}</h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Version</dt>
        <dd className="font-mono">{build.version}</dd>
        <dt className="text-muted-foreground">Commit</dt>
        <dd>
          <code className="font-mono" title={build.commit}>
            {build.commit.slice(0, 7)}
          </code>
        </dd>
        <dt className="text-muted-foreground">Built</dt>
        <dd className="font-mono">
          {build.builtAt}
          {!Number.isNaN(parsed) && (
            <span className="text-muted-foreground"> ({relativeTime(build.builtAt, now)})</span>
          )}
        </dd>
      </dl>
    </div>
  );
}

export function BuildCard({ api, dashboard, now }: { api: Build | undefined; dashboard: Build; now: number }) {
  return (
    <Card role="region" aria-label="Build">
      <CardHeader>
        <CardTitle className="text-xl leading-[1.2] font-semibold">Build</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          {api ? <BuildColumn title="API" build={api} now={now} /> : <Skeleton className="h-24 w-full" />}
          <BuildColumn title="Dashboard" build={dashboard} now={now} />
        </div>
      </CardContent>
    </Card>
  );
}
```
`web/src/features/status/SignedInCard.tsx`

```tsx
import { RoleBadge } from "@/components/RoleBadge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/useMe";

export function SignedInCard() {
  const me = useMe();
  return (
    <Card role="region" aria-label="Signed in as">
      <CardHeader>
        <CardTitle className="text-xl leading-[1.2] font-semibold">Signed in as</CardTitle>
      </CardHeader>
      <CardContent>
        {me.data ? (
          <div className="flex flex-wrap items-center gap-3">
            <Avatar className="size-10">
              <AvatarImage src={me.data.avatarUrl} alt="" />
              <AvatarFallback>{me.data.login.slice(0, 1).toUpperCase()}</AvatarFallback>
            </Avatar>
            <span className="text-sm font-semibold">{me.data.login}</span>
            <RoleBadge role={me.data.role} />
            {(me.data.orgs ?? []).length > 0 && (
              <span className="text-sm text-muted-foreground">
                Access granted by {(me.data.orgs ?? []).join(", ")}
              </span>
            )}
          </div>
        ) : (
          <Skeleton className="h-10 w-full max-w-sm" />
        )}
      </CardContent>
    </Card>
  );
}
```
`web/src/features/status/StatusError.tsx`

```tsx
import { useEffect, useRef, useState } from "react";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const COPIED_MS = 2000;

// Shown when /api/status fails with no cached data. The trace id lets an SRE
// paste the failed request straight into Grafana.
export function StatusError({ traceId, onRetry }: { traceId: string | null; onRetry: () => void }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = async () => {
    if (!traceId) return;
    try {
      await navigator.clipboard.writeText(traceId);
    } catch {
      return; // Clipboard unavailable: the id is still visible and selectable.
    }
    setCopied(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), COPIED_MS);
  };

  return (
    <div role="alert" className="flex flex-col gap-3 text-sm">
      <p className="text-xl leading-[1.2] font-semibold">Couldn&apos;t load telemetry status</p>
      <p className="flex flex-wrap items-center gap-x-1">
        <span>
          The request to the API failed. Select Retry Loading Status
          {traceId ? ", or look up trace " : "."}
        </span>
        {traceId && (
          <>
            <code className="font-mono">{traceId}</code>
            <span>in Grafana.</span>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Copy trace ID"
                    onClick={() => void copy()}
                  >
                    <Copy aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{copied ? "Trace ID copied" : "Copy trace ID"}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </>
        )}
      </p>
      <span aria-live="polite" className="sr-only">
        {copied ? "Trace ID copied" : ""}
      </span>
      <div>
        <Button variant="outline" onClick={onRetry}>
          Retry Loading Status
        </Button>
      </div>
    </div>
  );
}
```
`web/src/features/status/StatusPage.tsx`

```tsx
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { api, ApiError, pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { dashboardBuild } from "@/buildinfo";
import { BuildCard } from "./BuildCard";
import { ServiceTable } from "./ServiceTable";
import { SignedInCard } from "./SignedInCard";
import { StatusError } from "./StatusError";

// Re-render every second so relative times keep ticking between 15s polls.
function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function StatusPage() {
  const now = useNow();
  // retry: false is deliberate: failures show once, and the user retries explicitly.
  const status = useQuery({
    queryKey: ["status"],
    refetchInterval: 15000,
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/status");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });

  // The first status render (or failure) ends the page-load span.
  useEffect(() => {
    if (status.isSuccess || status.isError) pageLoadDone();
  }, [status.isSuccess, status.isError]);

  const staleFor =
    status.isError && status.data ? Math.max(0, Math.round((now - status.dataUpdatedAt) / 1000)) : null;
  const errorTrace = status.error instanceof ApiError ? status.error.traceId : null;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <PageHeading>Telemetry status</PageHeading>
        {status.data ? (
          <Button asChild>
            <a href={status.data.grafanaUrl} target="_blank" rel="noopener noreferrer">
              <ExternalLink aria-hidden="true" />
              Open in Grafana
            </a>
          </Button>
        ) : (
          <Skeleton className="h-11 w-40" />
        )}
      </div>

      <SignedInCard />
      <BuildCard api={status.data?.build.api} dashboard={dashboardBuild} now={now} />

      <Card role="region" aria-label="Service telemetry">
        <CardHeader className="flex flex-row items-center justify-between gap-4">
          <CardTitle className="text-xl leading-[1.2] font-semibold">Service telemetry</CardTitle>
          {staleFor !== null && (
            <span role="status" className="text-sm text-muted-foreground">
              Updated {staleFor}s ago
            </span>
          )}
        </CardHeader>
        <CardContent>
          {status.data ? (
            <ServiceTable
              services={status.data.services ?? []}
              now={now}
              onRetry={() => void status.refetch()}
            />
          ) : status.isError ? (
            <StatusError traceId={errorTrace} onRetry={() => void status.refetch()} />
          ) : (
            <div aria-busy="true" className="flex flex-col gap-2">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-[26rem] w-full" />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```
`web/src/features/status/status.test.tsx`

```tsx
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/schema";
import { HealthBadge } from "./HealthBadge";
import { ServiceTable } from "./ServiceTable";
import { StatusError } from "./StatusError";
import { relativeTime } from "./relativeTime";

type Service = components["schemas"]["ServiceStatus"];

// Radix tooltips load a second copy of React under Bazel's symlinked node_modules
// ("Invalid hook call"), so unit tests stub the tooltip primitives. The real tooltip is
// exercised by the Playwright status spec.
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <span data-testid="tooltip">{children}</span>
  ),
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const SPEC: Array<[string, string, Service["group"]]> = [
  ["api", "API", "platform"],
  ["worker", "Worker", "platform"],
  ["dashboard", "Dashboard", "platform"],
  ["postgres", "Postgres", "data"],
  ["redis", "Redis", "data"],
  ["otel-collector", "OTel Collector", "pipeline"],
  ["tempo", "Tempo", "backends"],
  ["loki", "Loki", "backends"],
  ["prometheus", "Prometheus", "backends"],
  ["grafana", "Grafana", "backends"],
];

function services(): Service[] {
  return SPEC.map(([id, name, group]) => ({
    id,
    name,
    group,
    status: "healthy",
    lastSignalAt: "2026-10-12T12:00:00Z",
    detail: "",
  }));
}

describe("relativeTime", () => {
  const now = Date.parse("2026-10-12T12:00:00Z");
  it("formats seconds", () => {
    expect(relativeTime(new Date(now - 12_000).toISOString(), now)).toBe("12s ago");
  });
  it("formats minutes", () => {
    expect(relativeTime(new Date(now - 3 * 60_000).toISOString(), now)).toBe("3m ago");
  });
  it("renders a dash for no signal", () => {
    expect(relativeTime(null, now)).toBe("—");
    expect(relativeTime(undefined, now)).toBe("—");
  });
});

describe("HealthBadge", () => {
  it.each([
    ["healthy", "Healthy"],
    ["degraded", "Degraded"],
    ["down", "Down"],
    ["unknown", "Unknown"],
  ] as const)("renders icon and text for %s", (status, label) => {
    const { container } = render(<HealthBadge status={status} />);
    expect(screen.getByText(label)).toBeTruthy();
    expect(container.querySelector("svg")).toBeTruthy();
  });
});

describe("ServiceTable", () => {
  it("renders four group headers in fixed order and ten rows", () => {
    const { container } = render(<ServiceTable services={services()} now={Date.now()} />);
    const headers = [...container.querySelectorAll("[data-group-header]")].map((e) =>
      e.textContent?.trim(),
    );
    expect(headers).toEqual(["Platform", "Data", "Telemetry pipeline", "Observability backends"]);
    const names = [...container.querySelectorAll("[data-service-row] [data-service-name]")].map(
      (e) => e.textContent?.trim(),
    );
    expect(names).toEqual(SPEC.map(([, name]) => name));
    // thead plus one body per group.
    expect(screen.getAllByRole("rowgroup")).toHaveLength(5);
  });

  it("shows the absolute timestamp in the title of the last signal", () => {
    const { container } = render(<ServiceTable services={services()} now={Date.parse("2026-10-12T12:00:12Z")} />);
    const cell = container.querySelector("[data-service-row] [data-last-signal]");
    expect(cell?.textContent).toBe("12s ago");
    expect(cell?.getAttribute("title")).toBe("2026-10-12T12:00:00Z");
  });

  it("renders the empty state for an empty list", () => {
    render(<ServiceTable services={[]} now={Date.now()} />);
    expect(screen.getByText("No service data yet")).toBeTruthy();
    expect(
      screen.getByText(
        "The API has not received telemetry from any service. Check that the OTel Collector is running, then select Retry Loading Status.",
      ),
    ).toBeTruthy();
  });
});

describe("StatusError", () => {
  const TRACE = "0123456789abcdef0123456789abcdef";

  it("shows the heading, the trace id in mono and a retry button", () => {
    const onRetry = vi.fn();
    render(<StatusError traceId={TRACE} onRetry={onRetry} />);
    expect(screen.getByText("Couldn't load telemetry status")).toBeTruthy();
    const id = screen.getByText(TRACE);
    expect(id.className).toContain("font-mono");
    fireEvent.click(screen.getByRole("button", { name: "Retry Loading Status" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("copies the trace id and announces it for two seconds", async () => {
    vi.useFakeTimers();
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const { container } = render(<StatusError traceId={TRACE} onRetry={() => {}} />);
    const live = container.querySelector('[aria-live="polite"]') as HTMLElement;
    expect(live).toBeTruthy();
    expect(live.textContent).toBe("");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy trace ID" }));
    });
    expect(writeText).toHaveBeenCalledWith(TRACE);
    expect(live.textContent).toBe("Trace ID copied");

    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    expect(live.textContent).toBe("");
  });

  it("omits the copy button when there is no trace id", () => {
    render(<StatusError traceId={null} onRetry={() => {}} />);
    expect(screen.queryByRole("button", { name: "Copy trace ID" })).toBeNull();
  });
});
```
---

## 9. nginx, container image and Bazel wiring

### 9.1 nginx

nginx is the front door on `:8080`:

- `/` serves the SPA with `try_files ... /index.html` for client-side routing.
- `/api/` proxies to the API and passes `Origin` / `Sec-Fetch-Site` through unchanged so Go's `CrossOriginProtection` keeps working.
- `/otlp/v1/(traces|logs|metrics)` is a POST-only allowlist with a 1 MB cap, a per-IP rate limit, and `Cookie` / `Authorization` stripped before the Collector sees the request.
- `/healthz` is what the status prober calls for the "Dashboard" row.

The upstream is declared as a **named upstream** because a `proxy_pass` containing a variable needs a runtime `resolver` and returns 502 without one.

`deploy/nginx/default.conf`

```nginx
# Same-origin front door for the dashboard SPA, the API, and browser OTLP ingest.
# Included inside the http context by the official nginx image.

limit_req_zone $binary_remote_addr zone=otlp:10m rate=20r/s;

# A named upstream is resolved at startup. A bare host in a proxy_pass that uses a
# variable ($1 below) would need a runtime `resolver` and fail with 502 otherwise.
upstream otel_collector_http {
    server otel-collector:4318;
}

server {
    listen 8080;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    location = /healthz {
        default_type text/plain;
        return 200 "ok";
    }

    # API: Origin and Sec-Fetch-Site pass through unchanged (proxy_pass forwards
    # all client headers by default) so Go's CrossOriginProtection works.
    location /api/ {
        proxy_pass http://api:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    # Browser OTLP ingest: POST-only allowlist of three signal paths, size cap,
    # rate limit, and credentials stripped before reaching the Collector.
    location ~ ^/otlp/(v1/(traces|logs|metrics))$ {
        limit_except POST {
            deny all;
        }
        client_max_body_size 1m;
        limit_req zone=otlp burst=20 nodelay;
        proxy_set_header Cookie "";
        proxy_set_header Authorization "";
        proxy_pass http://otel_collector_http/$1;
    }

    # Any other /otlp path is not served.
    location /otlp/ {
        return 404;
    }

    location / {
        try_files $uri /index.html;
    }
}
```
### 9.2 Web image

`web/Dockerfile`

```dockerfile
# Build context is the repo root so deploy/nginx/default.conf is reachable:
#   docker build -f web/Dockerfile .
FROM node:24-alpine AS build
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
ENV VITE_BUILD_VERSION=$VERSION VITE_BUILD_COMMIT=$COMMIT VITE_BUILD_TIME=$BUILT_AT
RUN npm run build

FROM nginx:1.31-alpine
COPY --from=build /app/web/dist /usr/share/nginx/html
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
```
`web/.dockerignore`

```text
node_modules
dist
test-results
playwright-report
```
`web/Dockerfile.dockerignore`

```dockerfile
node_modules
dist
test-results
playwright-report
```
### 9.3 Bazel: `web/BUILD.bazel`

`web/BUILD.bazel`

```python
load("@aspect_rules_js//js:defs.bzl", "js_library")
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@npm//:defs.bzl", "npm_link_all_packages")
load("@npm//web:@playwright/test/package_json.bzl", playwright_bin = "bin")
load("@npm//web:openapi-typescript/package_json.bzl", openapi_typescript_bin = "bin")
load("@npm//web:typescript/package_json.bzl", typescript_bin = "bin")
load("@npm//web:vite/package_json.bzl", vite_bin = "bin")
load("@npm//web:vitest/package_json.bzl", vitest_bin = "bin")

# npm stays the package manager: package.json + package-lock.json are the source of truth.
# rules_js derives pnpm-lock.yaml from package-lock.json; commit it alongside.
npm_link_all_packages(name = "node_modules")

exports_files([
    "package.json",
    "package-lock.json",
    "pnpm-lock.yaml",
    "pnpm-workspace.yaml",
])

js_library(
    name = "srcs",
    srcs = glob(
        [
            "src/**/*.ts",
            "src/**/*.tsx",
            "src/**/*.css",
        ],
        exclude = [
            "src/**/*.test.ts",
            "src/**/*.test.tsx",
        ],
    ) + [
        "index.html",
        "components.json",
    ],
)

js_library(
    name = "configs",
    srcs = [
        "package.json",
        "tsconfig.app.json",
        "tsconfig.json",
        "tsconfig.node.json",
        "vite.config.ts",
        "vitest.config.ts",
        "vitest.bazel.config.ts",
        "playwright.config.ts",
    ],
)

_NODE_MODULES = [":node_modules"]

_UNIT_TESTS = glob(
    [
        "src/**/*.test.ts",
        "src/**/*.test.tsx",
    ],
    allow_empty = True,
)

_E2E = glob(
    ["e2e/**"],
    allow_empty = True,
)

# ---------------------------------------------------------------- build (npm run build)
# Type check: the `tsc -b` half of `npm run build`.
typescript_bin.tsc_test(
    name = "typecheck",
    args = [
        "-b",
        "--noEmit",
    ],
    chdir = package_name(),
    # .d.ts sources (src/vite-env.d.ts) are types, which js_test omits from runfiles by default.
    include_types = True,
    data = [
        ":configs",
        ":srcs",
    ] + _UNIT_TESTS + _E2E + _NODE_MODULES,
)

# Production SPA bundle: the `vite build` half of `npm run build` -> web/dist.
# Container images are still built by web/Dockerfile (docker compose).
vite_bin.vite(
    name = "dist",
    srcs = [
        ":configs",
        ":srcs",
    ] + _NODE_MODULES,
    args = ["build"],
    chdir = package_name(),
    # Rolldown (Vite 8) canonicalizes paths natively, which escapes the sandbox's symlink
    # tree and breaks index.html asset naming; build in the execroot instead.
    execution_requirements = {"no-sandbox": "1"},
    out_dirs = ["dist"],
)

# ---------------------------------------------------------------- tests
# Vitest unit suite (npm test -- --run).
vitest_bin.vitest_test(
    name = "test",
    args = [
        "run",
        "--config",
        "vitest.bazel.config.ts",
    ],
    chdir = package_name(),
    data = [
        ":configs",
        ":srcs",
    ] + _UNIT_TESTS + _NODE_MODULES,
)

# Playwright e2e against the running up-fake stack (http://localhost:8080).
# Needs `bazel run //:up_fake` first and the Playwright browsers installed on the host
# (`npm --prefix web exec -- playwright install chromium`); hence manual and unsandboxed.
#   bazel run //:e2e
playwright_bin.playwright_test(
    name = "e2e",
    args = ["test"],
    chdir = package_name(),
    data = [
        ":configs",
    ] + _E2E + _NODE_MODULES,
    tags = [
        "external",
        "local",
        "manual",
        "no-sandbox",
        "requires-docker",
        "requires-network",
    ],
    visibility = ["//:__pkg__"],
)

# ---------------------------------------------------------------- codegen
# openapi-typescript over the generated OpenAPI document (npm run gen).
openapi_typescript_bin.openapi_typescript(
    name = "schema_gen",
    srcs = ["//api:_gen/openapi.json"],
    outs = ["_gen/schema.d.ts"],
    args = [
        "../api/_gen/openapi.json",
        "-o",
        "_gen/schema.d.ts",
    ],
    chdir = package_name(),
)

write_source_files(
    name = "gen",
    files = {
        "src/api/schema.d.ts": ":_gen/schema.d.ts",
        "src/gen/conventions.gen.ts": "//internal/conventions:_gen/conventions.gen.ts",
    },
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)
```
### 9.4 `MODULE.bazel`: add the Node/web half

Add these two lines next to the other `bazel_dep` lines:

```python
bazel_dep(name = "aspect_rules_js", version = "3.5.1")
bazel_dep(name = "rules_nodejs", version = "6.7.6")
```

and append this section at the end of the file:

`MODULE.bazel`

```python
# ---------------------------------------------------------------- Node / web
node = use_extension("@rules_nodejs//nodejs:extensions.bzl", "node", dev_dependency = True)
node.toolchain(node_version = "24.20.0")

npm = use_extension("@aspect_rules_js//npm:extensions.bzl", "npm", dev_dependency = True)

# web/package.json and web/package-lock.json stay the dependency source of truth (npm).
# With update_pnpm_lock, rules_js re-derives web/pnpm-lock.yaml from package-lock.json
# (`pnpm import`) whenever its inputs change; the first build after a change updates the
# file and asks to re-run. Commit web/pnpm-lock.yaml and .aspect/rules/ with the change.
npm.npm_translate_lock(
    name = "npm",
    data = [
        "//web:package.json",
        "//web:pnpm-workspace.yaml",
    ],
    npm_package_lock = "//web:package-lock.json",
    pnpm_lock = "//web:pnpm-lock.yaml",
    update_pnpm_lock = True,
    verify_node_modules_ignored = "//:.bazelignore",
)
use_repo(npm, "npm")
```
### 9.5 Root `BUILD.bazel`: re-enable the `//web` entries

In the root `BUILD.bazel` from Phase 1b, add `"//web"` back to `_GEN_PACKAGES` and add the e2e alias at the bottom:

```python
_GEN_PACKAGES = [
    "//api",
    "//internal/conventions",
    "//internal/store",
    "//web",
]
```

```python
# Playwright e2e against the up_fake stack: bazel run //:e2e
alias(
    name = "e2e",
    actual = "//web:e2e",
    tags = ["manual"],
)
```

Then generate everything:

```bash
bazel run //:gen
```

---

## 10. End-to-end specs (Playwright)

These run against the real stack (`bazel run //:up_fake`) and the fake GitHub. They are the executable form of the success criteria: sign in and out, the denied page, all ten services healthy with a live browser, and one trace spanning dashboard → API → worker, then the Grafana correlation.

`web/e2e/helpers.ts`

```typescript
import { expect, type Page } from "@playwright/test";

// Drives the full sign-in: SPA login page -> API OAuth start -> (fake) GitHub -> callback -> "/".
export async function loginAs(page: Page, login: string): Promise<void> {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: login, exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/");
}
```
`web/e2e/auth.spec.ts`

```typescript
import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("unauthenticated visit redirects to sign-in", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: "Sign in to Ephematool" })).toBeVisible();
});

test("allowed member signs in and sees the status page", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  const menu = page.getByRole("menu");
  await expect(menu.getByText("octo-admin")).toBeVisible();
  await expect(menu.getByText("Admin", { exact: true })).toBeVisible();
});

test("sign out returns to sign-in", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
});

for (const login of ["octo-outsider", "octo-pending"]) {
  test(`denied member sees the denied page (${login})`, async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Continue with GitHub" }).click();
    await page.getByRole("button", { name: login, exact: true }).click();
    await expect(page).toHaveURL(/\/denied/);
    const heading = page.getByRole("heading", { name: "Access denied" });
    await expect(heading).toBeFocused();
    await expect(page.getByText(login, { exact: false }).first()).toBeVisible();
    await expect(page.getByText("ephematool-e2e").first()).toBeVisible();
    await page.getByRole("button", { name: "Try a different account" }).click();
    await expect(page).toHaveURL(/\/login/);
  });
}

test("cancelled GitHub sign-in shows failure copy", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByText("GitHub sign-in didn't complete. Try again.")).toBeVisible();
});

test("expired session redirects with message", async ({ page, context }) => {
  test.setTimeout(60_000);
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  await context.clearCookies();
  // The 15s status poll gets a 401, which sends the SRE to /login?reason=expired.
  await expect(page).toHaveURL(/\/login\?.*reason=expired/, { timeout: 30_000 });
  await expect(page.getByText("You've been signed out. Sign in to continue.")).toBeVisible();
});

test("session survives a browser restart", async ({ page, context, browser }) => {
  await loginAs(page, "octo-admin");
  const state = await context.storageState();
  await context.close();

  const restarted = await browser.newContext({ storageState: state });
  const page2 = await restarted.newPage();
  await page2.goto("/");
  await expect(page2.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  const cookie = (await restarted.cookies()).find((c) => c.name === "ephematool_session");
  expect(cookie).toBeDefined();
  expect(cookie!.expires).toBeGreaterThan(Date.now() / 1000 + 6 * 24 * 3600);
  expect(cookie!.httpOnly).toBe(true);
  expect(cookie!.sameSite).toBe("Lax");
  await restarted.close();
});

test("user menu theme and sign out", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitemradio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  expect(await page.evaluate(() => localStorage.getItem("ephematool-theme"))).toBe("dark");
  await page.keyboard.press("Escape");
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
});

test("viewer role badge", async ({ page }) => {
  await loginAs(page, "octo-viewer");
  await page.getByRole("button", { name: "Open user menu for octo-viewer" }).click();
  await expect(page.getByRole("menu").getByText("Viewer", { exact: true })).toBeVisible();
});
```
`web/e2e/status.spec.ts`

```typescript
import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const GROUPS = ["Platform", "Data", "Telemetry pipeline", "Observability backends"];
const SERVICES = [
  "API",
  "Worker",
  "Dashboard",
  "Postgres",
  "Redis",
  "OTel Collector",
  "Tempo",
  "Loki",
  "Prometheus",
  "Grafana",
];

test("status page lists 10 healthy services in four fixed groups", async ({ page }) => {
  test.setTimeout(120_000);
  await loginAs(page, "octo-admin");

  const table = page.getByRole("table");
  await expect(table).toBeVisible();

  // Group headers, in order.
  const groupHeaders = table.locator("[data-group-header]");
  await expect(groupHeaders).toHaveText(GROUPS);

  // Service rows, in order (first cell of each data row).
  const names = table.locator("[data-service-row] [data-service-name]");
  await expect(names).toHaveText(SERVICES);

  // Every row converges on Healthy (the first probe cycle can lag stack start).
  const badges = table.locator("[data-service-row] [data-health]");
  await expect(badges).toHaveCount(10);
  await expect(async () => {
    for (const text of await badges.allTextContents()) expect(text.trim()).toBe("Healthy");
  }).toPass({ timeout: 60_000 });

  const signedIn = page.getByRole("region", { name: "Signed in as" });
  await expect(signedIn).toContainText("octo-admin");
  await expect(signedIn).toContainText("Admin");
  await expect(signedIn).toContainText("ephematool-e2e");

  const build = page.getByRole("region", { name: "Build" });
  await expect(build).toContainText("API");
  await expect(build).toContainText("Dashboard");
});

test("failed status request shows the trace id and retries exactly once per press", async ({
  page,
}) => {
  await loginAs(page, "octo-admin");

  let requests = 0;
  await page.route("**/api/status", (route) => {
    requests += 1;
    return route.fulfill({
      status: 500,
      headers: { "X-Trace-Id": "0123456789abcdef0123456789abcdef" },
    });
  });

  await page.reload();
  await expect(page.getByText("Couldn't load telemetry status")).toBeVisible();
  await expect(page.getByText("0123456789abcdef0123456789abcdef")).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy trace ID" })).toBeVisible();

  // No silent retries: the failed load issued exactly one request.
  await page.waitForTimeout(500);
  const before = requests;
  expect(before).toBe(1);

  await page.getByRole("button", { name: "Retry Loading Status" }).click();
  await expect.poll(() => requests).toBe(before + 1);
  await page.waitForTimeout(500);
  expect(requests).toBe(before + 1);
});
```
`web/e2e/telemetry.spec.ts`

```typescript
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const TEMPO = "http://127.0.0.1:3200";
const WANTED = ["ephematool-dashboard", "ephematool-api", "ephematool-worker"];

type Attr = { key: string; value?: { stringValue?: string } };
type Batch = { resource?: { attributes?: Attr[] } };
type TraceDoc = { batches?: Batch[]; resourceSpans?: Batch[] };

function serviceNames(doc: TraceDoc): string[] {
  const batches = [...(doc.batches ?? []), ...(doc.resourceSpans ?? [])];
  const names = new Set<string>();
  for (const b of batches) {
    for (const a of b.resource?.attributes ?? []) {
      if (a.key === "service.name" && a.value?.stringValue) names.add(a.value.stringValue);
    }
  }
  return [...names];
}

test("one page load is one trace across dashboard, api and worker", async ({ page, request }) => {
  test.setTimeout(120_000);

  await loginAs(page, "octo-admin");

  // Reload so the page load starts with a session; capture its first status request.
  const statusRequest = page.waitForRequest((r) => new URL(r.url()).pathname === "/api/status");
  await page.reload();
  const first = await statusRequest;
  const traceparent = first.headers()["traceparent"];
  expect(traceparent).toMatch(/^00-[0-9a-f]{32}-/);
  const traceId = traceparent.split("-")[1];

  let seen: string[] = [];
  await expect
    .poll(
      async () => {
        const res = await request.get(`${TEMPO}/api/traces/${traceId}`);
        if (!res.ok()) return false;
        seen = serviceNames((await res.json()) as TraceDoc);
        return WANTED.every((s) => seen.includes(s));
      },
      {
        message: `trace ${traceId} should contain ${WANTED.join(", ")} (saw: ${seen.join(", ") || "none"})`,
        intervals: [2_000],
        timeout: 45_000,
      },
    )
    .toBe(true);

  // Under `bazel run` the cwd is the runfiles tree; hand off into the real checkout.
  const webDir = process.env.BUILD_WORKSPACE_DIRECTORY
    ? join(process.env.BUILD_WORKSPACE_DIRECTORY, "web")
    : process.cwd();
  mkdirSync(join(webDir, "test-results"), { recursive: true });
  writeFileSync(join(webDir, "test-results", "dashboard-trace-id.txt"), `${traceId}\n`);
});
```
`web/e2e/grafana.spec.ts`

```typescript
import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

// Grafana is published on the host, not behind nginx, so it uses an absolute URL (not baseURL).
const GRAFANA = "http://127.0.0.1:3000";

test("provisioned platform dashboard loads", async ({ page }) => {
  await page.goto(`${GRAFANA}/d/ephematool-platform`);
  await expect(page.getByText("Request rate").first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("p95 latency").first()).toBeVisible();
  await expect(page.getByText("Worker tasks").first()).toBeVisible();
  await expect(page.getByText("Auth outcomes").first()).toBeVisible();
});

test("Loki log line links to its Tempo trace and the trace links back to logs (A5)", async ({
  page,
}) => {
  test.setTimeout(180_000);

  // Produce api logs that carry a trace_id.
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("table")).toBeVisible();
  await page.waitForTimeout(3_000);

  const panes = {
    a: {
      datasource: "loki",
      queries: [
        {
          refId: "A",
          expr: '{service_name="ephematool-api"} | trace_id != ""',
          queryType: "range",
          datasource: { type: "loki", uid: "loki" },
        },
      ],
      range: { from: "now-15m", to: "now" },
    },
  };
  const url = `${GRAFANA}/explore?schemaVersion=1&orgId=1&panes=${encodeURIComponent(
    JSON.stringify(panes),
  )}`;
  await page.goto(url);

  // Expand the first log row (Loki can lag a few seconds behind the collector).
  const row = page.locator('[role="row"], [data-testid="logRows"] > div').filter({
    hasText: /ephematool|auth|status|request/i,
  });
  await expect(row.first()).toBeVisible({ timeout: 60_000 });
  await row.first().click();

  // The trace_id derived field exposes a link/button into the Tempo datasource.
  const tempoLink = page.getByRole("button", { name: /^Tempo$/ }).or(
    page.getByRole("link", { name: /^Tempo$/ }),
  );
  await expect(tempoLink.first()).toBeVisible({ timeout: 30_000 });
  await tempoLink.first().click();

  // The trace view opens in the split pane and shows the api service.
  const traceView = page.getByText("Trace ID").first();
  await expect(traceView).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("ephematool/ephematool-api").first()).toBeVisible();

  // Expand the root span, then follow the tracesToLogsV2 link ("Logs for this trace" in
  // Grafana 13, "Logs for this span" in older builds) back to Loki.
  // The root span row is the last match for the service-and-operation label.
  await page.getByText("ephematool/ephematool-api").last().click();
  const logsLink = page
    .getByRole("button", { name: /Logs for this (trace|span)/i })
    .or(page.getByRole("link", { name: /Logs for this (trace|span)/i }));
  await expect(logsLink.first()).toBeVisible({ timeout: 30_000 });
  await logsLink.first().click();

  // The link opens a Loki query scoped to this trace.
  await expect(page.getByText(/Logs volume/i).first()).toBeVisible({ timeout: 30_000 });
});
```
---

## 11. Build, test, run

```bash
bazel test //web:test //web:typecheck          # unit tests + tsc
bazel build //web:dist                          # production bundle
bazel run //:up_fake                            # whole stack with the fake GitHub
npm --prefix web exec -- playwright install chromium   # once
bazel run //:e2e                                # Playwright
bazel run //:verify_telemetry                   # every verify.d check
```

Open http://localhost:8080. Choose a fixture user (the fake GitHub renders a small login form): `octo-admin` signs in as admin; `octo-outsider` lands on the denied page.

## 12. Phase 1 acceptance checklist (the whole of Phase 1)

- [ ] `bazel test //...` passes (unit tests, typecheck, codegen drift check `//:check_gen`).
- [ ] `bazel test --config=integration //:test_integration` passes.
- [ ] `bazel run //:up_fake` brings up every service healthy with **one** command (SC3).
- [ ] Sign in as `octo-admin` → status page shows ten rows; close the browser, reopen → still signed in (D-03); sign out works.
- [ ] `octo-outsider`, `octo-pending` and `octo-forbidden` all land on `/denied` (for `octo-forbidden` the API also logs `auth.org_check_forbidden`, the sign of an org that restricts third-party OAuth apps).
- [ ] After one page load, Grafana → Explore → Tempo has **one** trace containing browser, API and worker spans, with correlated Loki logs and a metric on the Ephematool dashboard (SC2).
- [ ] `bazel run //:verify_telemetry` passes, including `redaction` (no cookie, token or secret in stored telemetry).
- [ ] `bazel run //:verify_fresh` passes from a clean clone (commit first).
- [ ] You can explain, without looking: why the browser OTLP path is same-origin, what `setPageLoadContext` does, and why `vitest.bazel.config.ts` sets `dedupe`.

## 13. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `Cannot find module './schema'` / type errors on `api.GET` | `schema.d.ts` not generated | `bazel run //web:gen` (needs `api/openapi.json` from Phase 1a). |
| Bazel says to re-run after the first web build | `rules_js` just updated `pnpm-lock.yaml` | re-run the same command; commit `web/pnpm-lock.yaml` and `.aspect/`. |
| Vite build fails with a path/asset naming error under Bazel | Rolldown escapes the sandbox symlink tree | the `no-sandbox` execution requirement on `//web:dist` is required; keep it. |
| `Invalid hook call` in Bazel tests only | React resolved twice | keep `dedupe` in `vitest.bazel.config.ts`. |
| Browser traces are separate from API traces | `setPageLoadContext` not called before the first fetch, or nginx drops `traceparent` | check `main.tsx` order and that nginx forwards request headers on `/api/`. |
| OTLP POSTs return 404 | path not under `/otlp/v1/{traces,logs,metrics}` | nginx only allows those three. |
| Playwright cannot find a browser | browsers not installed | `npm --prefix web exec -- playwright install chromium`. |
| Everything works at `:8080` but login loops | `PUBLIC_BASE_URL` not `http://localhost:8080` | it must match the browser origin because it builds the OAuth callback URL. |
