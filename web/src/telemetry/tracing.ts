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
