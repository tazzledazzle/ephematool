import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { api, ApiError, pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { dashboardBuild } from "@/buildinfo";
import { BuildCard } from "./BuildCard";
import { ServiceTable } from "./ServiceTable";
import { SignedInCard } from "./SignedInCard";
import { StatusError } from "./StatusError";

// Re-render every second so relative times keep ticking between 15s polls.
function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function StatusPage() {
  const now = useNow();
  // retry: false is deliberate: failures show once, and the user retries explicitly.
  const status = useQuery({
    queryKey: ["status"],
    refetchInterval: 15000,
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/status");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });

  // The first status render (or failure) ends the page-load span.
  useEffect(() => {
    if (status.isSuccess || status.isError) pageLoadDone();
  }, [status.isSuccess, status.isError]);

  const staleFor =
    status.isError && status.data ? Math.max(0, Math.round((now - status.dataUpdatedAt) / 1000)) : null;
  const errorTrace = status.error instanceof ApiError ? status.error.traceId : null;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <PageHeading>Telemetry status</PageHeading>
        {status.data ? (
          <Button asChild>
            <a href={status.data.grafanaUrl} target="_blank" rel="noopener noreferrer">
              <ExternalLink aria-hidden="true" />
              Open in Grafana
            </a>
          </Button>
        ) : (
          <Skeleton className="h-11 w-40" />
        )}
      </div>

      <SignedInCard />
      <BuildCard api={status.data?.build.api} dashboard={dashboardBuild} now={now} />

      <Card role="region" aria-label="Service telemetry">
        <CardHeader className="flex flex-row items-center justify-between gap-4">
          <CardTitle className="text-xl leading-[1.2] font-semibold">Service telemetry</CardTitle>
          {staleFor !== null && (
            <span role="status" className="text-sm text-muted-foreground">
              Updated {staleFor}s ago
            </span>
          )}
        </CardHeader>
        <CardContent>
          {status.data ? (
            <ServiceTable
              services={status.data.services ?? []}
              now={now}
              onRetry={() => void status.refetch()}
            />
          ) : status.isError ? (
            <StatusError traceId={errorTrace} onRetry={() => void status.refetch()} />
          ) : (
            <div aria-busy="true" className="flex flex-col gap-2">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-[26rem] w-full" />
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
