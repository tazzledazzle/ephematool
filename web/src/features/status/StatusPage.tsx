import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { api, ApiError, pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { dashboardBuild } from "@/buildinfo";

type Build = { version: string; commit: string; builtAt: string };

function BuildColumn({ title, build }: { title: string; build: Build }) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-xs leading-normal font-semibold text-muted-foreground">{title}</h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Version</dt>
        <dd className="font-mono">{build.version}</dd>
        <dt className="text-muted-foreground">Commit</dt>
        <dd>
          <code className="font-mono" title={build.commit}>
            {build.commit.slice(0, 7)}
          </code>
        </dd>
        <dt className="text-muted-foreground">Built</dt>
        <dd className="font-mono">{build.builtAt}</dd>
      </dl>
    </div>
  );
}

export function StatusPage() {
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

      {status.isError && (
        <div role="alert" className="flex flex-col gap-3 text-sm">
          <p className="text-xl leading-[1.2] font-semibold">Couldn&apos;t load telemetry status</p>
          <p>
            The request to the API failed.
            {status.error instanceof ApiError && status.error.traceId && (
              <>
                {" "}
                Look up trace <code className="font-mono">{status.error.traceId}</code> in Grafana.
              </>
            )}
          </p>
          <div>
            <Button variant="outline" onClick={() => void status.refetch()}>
              Retry Loading Status
            </Button>
          </div>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-xl leading-[1.2] font-semibold">Build</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            {status.data ? (
              <BuildColumn title="API" build={status.data.build.api} />
            ) : (
              <Skeleton className="h-24 w-full" />
            )}
            <BuildColumn title="Dashboard" build={dashboardBuild} />
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
