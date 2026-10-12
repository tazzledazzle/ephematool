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
