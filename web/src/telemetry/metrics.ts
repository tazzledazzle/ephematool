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
