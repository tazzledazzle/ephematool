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
