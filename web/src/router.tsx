import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "@/components/AppShell";
import { DeniedPage } from "@/features/auth/DeniedPage";
import { LoginPage } from "@/features/auth/LoginPage";
import { StatusPage } from "@/features/status/StatusPage";

const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

const rootRoute = createRootRoute({ component: Outlet });

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (s: Record<string, unknown>): { reason?: string; next?: string } => ({
    reason: str(s.reason),
    next: str(s.next),
  }),
  component: LoginPage,
});

const deniedRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/denied",
  validateSearch: (s: Record<string, unknown>): { reason?: string; login?: string } => ({
    reason: str(s.reason),
    login: str(s.login),
  }),
  component: DeniedPage,
});

// Pathless layout route: everything inside needs a signed-in user.
const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: AppShell,
});

const statusRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: StatusPage,
});

const routeTree = rootRoute.addChildren([loginRoute, deniedRoute, shellRoute.addChildren([statusRoute])]);

export const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
