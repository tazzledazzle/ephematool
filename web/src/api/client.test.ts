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
