import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { relativeTime } from "./relativeTime";

export type Build = { version: string; commit: string; builtAt: string };

function BuildColumn({ title, build, now }: { title: string; build: Build; now: number }) {
  const parsed = Date.parse(build.builtAt);
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
        <dd className="font-mono">
          {build.builtAt}
          {!Number.isNaN(parsed) && (
            <span className="text-muted-foreground"> ({relativeTime(build.builtAt, now)})</span>
          )}
        </dd>
      </dl>
    </div>
  );
}

export function BuildCard({ api, dashboard, now }: { api: Build | undefined; dashboard: Build; now: number }) {
  return (
    <Card role="region" aria-label="Build">
      <CardHeader>
        <CardTitle className="text-xl leading-[1.2] font-semibold">Build</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
          {api ? <BuildColumn title="API" build={api} now={now} /> : <Skeleton className="h-24 w-full" />}
          <BuildColumn title="Dashboard" build={dashboard} now={now} />
        </div>
      </CardContent>
    </Card>
  );
}
