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
