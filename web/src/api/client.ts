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
