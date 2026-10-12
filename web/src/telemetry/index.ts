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
