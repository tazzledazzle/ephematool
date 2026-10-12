import { useEffect } from "react";
import { Navigate, useSearch } from "@tanstack/react-router";
import { pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";
import { useMe } from "./useMe";

function GithubMark() {
  return (
    <svg aria-hidden="true" viewBox="0 0 16 16" fill="currentColor" className="size-5">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}

export function LoginPage() {
  const search = useSearch({ strict: false }) as { reason?: string; next?: string };
  const me = useMe();

  // This page has no status request, so the page load ends once identity is known.
  useEffect(() => {
    if (me.isSuccess || me.isError) pageLoadDone();
  }, [me.isSuccess, me.isError]);

  // Already signed in: go straight to the app.
  if (me.isSuccess) return <Navigate to="/" replace />;

  const start = () => {
    // Full-page navigation into the OAuth flow; never fetch (the provider redirects).
    window.location.assign("/api/auth/github/login?next=" + encodeURIComponent(search.next ?? "/"));
  };

  return (
    <main className="min-h-screen bg-background px-4 pt-16">
      <div className="mx-auto flex w-full max-w-[400px] flex-col gap-4 rounded-xl border bg-card p-8 text-card-foreground">
        <p className="text-xs leading-normal font-semibold text-muted-foreground">Ephematool</p>
        <PageHeading role="display">Sign in to Ephematool</PageHeading>
        {search.reason === "expired" && (
          <p className="text-sm leading-normal">You&apos;ve been signed out. Sign in to continue.</p>
        )}
        {search.reason === "failed" && (
          <p role="alert" className="text-sm leading-normal text-destructive">
            GitHub sign-in didn&apos;t complete. Try again.
          </p>
        )}
        <p className="text-sm leading-normal text-muted-foreground">
          Use your GitHub account to continue. Access is limited to approved organizations.
        </p>
        <Button className="min-h-11 w-full" onClick={start}>
          <GithubMark />
          Continue with GitHub
        </Button>
      </div>
    </main>
  );
}
