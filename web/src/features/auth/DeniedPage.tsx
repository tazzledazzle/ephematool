import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { api, ApiError, pageLoadDone } from "@/api/client";
import { PageHeading } from "@/components/PageHeading";
import { Button } from "@/components/ui/button";

// Public page: it must not depend on a session. `login` comes from the URL and is
// rendered as React text only; the required orgs come from the API, never the URL.
export function DeniedPage() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as { login?: string };
  const cfg = useQuery({
    queryKey: ["auth-config"],
    retry: false,
    queryFn: async () => {
      const { data, response } = await api.GET("/api/auth/config");
      if (!response.ok || !data) throw ApiError.fromResponse(response);
      return data;
    },
  });

  useEffect(() => {
    if (cfg.isSuccess || cfg.isError) pageLoadDone();
  }, [cfg.isSuccess, cfg.isError]);

  const required = [...(cfg.data?.allowedOrgs ?? []), ...(cfg.data?.allowedTeams ?? [])];

  return (
    <main className="min-h-screen bg-background px-4 pt-16">
      <div className="mx-auto flex w-full max-w-[400px] flex-col gap-4 rounded-xl border bg-card p-8 text-card-foreground">
        <PageHeading role="display">Access denied</PageHeading>
        <p className="text-sm leading-normal">
          Your GitHub account ({search.login ?? "unknown"}) is not a member of{" "}
          {required.length === 0
            ? "an approved organization"
            : required.map((r, i) => (
                <span key={r}>
                  {i > 0 && " or "}
                  <code className="font-mono">{r}</code>
                </span>
              ))}
          . Ask an owner of that organization to add you, then sign in again.
        </p>
        <Button variant="secondary" className="min-h-11 w-full" onClick={() => void navigate({ to: "/login" })}>
          Try a different account
        </Button>
      </div>
    </main>
  );
}
