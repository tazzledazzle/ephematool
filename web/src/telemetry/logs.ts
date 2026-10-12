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
