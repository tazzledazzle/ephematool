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
