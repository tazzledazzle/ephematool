import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, Navigate, Outlet, useNavigate } from "@tanstack/react-router";
import { api, ApiError } from "@/api/client";
import { UserMenu } from "@/components/UserMenu";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/useMe";

// Rendered from an array so later phases add items without touching the markup.
const NAV_ITEMS = [{ to: "/", label: "Telemetry" }] as const;

export function AppShell() {
  const me = useMe();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [signOutFailed, setSignOutFailed] = useState(false);

  const signOut = async () => {
    setSignOutFailed(false);
    try {
      const { response } = await api.POST("/api/auth/logout");
      if (!response.ok) throw ApiError.fromResponse(response);
      queryClient.clear();
      await navigate({ to: "/login", replace: true });
    } catch {
      setSignOutFailed(true);
    }
  };

  // Not signed in: leave the shell (no flash of content, no extra toast).
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" replace />;
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="fixed inset-x-0 top-0 z-10 flex h-14 items-center gap-6 border-b bg-secondary px-6">
        <span className="text-xl leading-[1.2] font-semibold">Ephematool</span>
        <nav aria-label="Primary">
          <ul className="flex items-center gap-4">
            {NAV_ITEMS.map((item) => (
              <li key={item.to}>
                <Link
                  to={item.to}
                  className="inline-flex h-14 items-center border-b-2 border-transparent text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:border-primary aria-[current=page]:text-primary"
                  activeOptions={{ exact: true }}
                >
                  {item.label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>
        <div className="ml-auto flex items-center gap-3">
          {me.isPending && <Skeleton className="size-8 rounded-full" />}
          {me.data && (
            <UserMenu
              login={me.data.login}
              avatarUrl={me.data.avatarUrl}
              role={me.data.role}
              onSignOut={signOut}
            />
          )}
        </div>
      </header>
      <main className="mx-auto w-full max-w-[1024px] px-6 pt-[104px] pb-12">
        {signOutFailed && (
          <p role="alert" className="mb-4 text-sm text-destructive">
            Sign out failed. Try again.
          </p>
        )}
        {me.isPending && (
          <div className="flex flex-col gap-6" aria-busy="true">
            <Skeleton className="h-7 w-64" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}
        {me.isError && !(me.error instanceof ApiError && me.error.status === 401) && (
          <div role="alert" className="flex flex-col gap-3 text-sm">
            <p>Couldn&apos;t load your account.</p>
            {me.error instanceof ApiError && me.error.traceId && (
              <p>
                Trace <code className="font-mono">{me.error.traceId}</code>
              </p>
            )}
            <div>
              <Button variant="outline" onClick={() => void me.refetch()}>
                Retry
              </Button>
            </div>
          </div>
        )}
        {/* Status loads only once identity is known, so an anonymous visit never
            fires a 401 that would look like an expired session. */}
        {me.isSuccess && <Outlet />}
      </main>
    </div>
  );
}
