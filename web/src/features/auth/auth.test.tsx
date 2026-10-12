import { createContext, useContext } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserMenu } from "@/components/UserMenu";
import { ThemeProvider } from "@/lib/ThemeProvider";
import { THEME_STORAGE_KEY } from "@/lib/theme";
import { DeniedPage } from "./DeniedPage";
import { LoginPage } from "./LoginPage";

const nav = vi.hoisted(() => ({ search: {} as Record<string, string>, navigate: vi.fn() }));

vi.mock("@tanstack/react-router", () => ({
  useSearch: () => nav.search,
  useNavigate: () => nav.navigate,
  Navigate: () => null,
}));

vi.mock("./useMe", () => ({
  useMe: () => ({ isSuccess: false, isError: true }),
}));

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");
  return {
    ...actual,
    pageLoadDone: vi.fn(),
    api: {
      GET: vi.fn(async () => ({
        data: { githubConfigured: true, allowedOrgs: ["ephematool-e2e"], allowedTeams: ["acme/sre"] },
        response: new Response(null, { status: 200 }),
      })),
      POST: vi.fn(async () => ({ response: new Response(null, { status: 204 }) })),
    },
  };
});

// Radix tooltips load a second copy of React under Bazel's symlinked node_modules
// ("Invalid hook call"), so unit tests stub the tooltip primitives (same as status.test.tsx).
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/avatar", () => ({
  Avatar: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
  AvatarImage: () => null,
  AvatarFallback: ({ children }: { children?: React.ReactNode }) => <span>{children}</span>,
}));

// Radix avatar and menus also load their own React copy under Bazel ("Invalid hook call"), so
// the avatar and dropdown primitives are stubbed with a minimal always-open equivalent. The real menu (open, keyboard,
// stays open on select) is exercised by the Playwright auth spec.
vi.mock("@/components/ui/dropdown-menu", () => {
  type Group = { value: string; onValueChange: (v: string) => void };
  const GroupCtx = createContext<Group | null>(null);
  const Pass = ({ children }: { children?: React.ReactNode }) => <>{children}</>;
  return {
    DropdownMenu: Pass,
    DropdownMenuTrigger: Pass,
    DropdownMenuContent: ({ children }: { children?: React.ReactNode }) => <div role="menu">{children}</div>,
    DropdownMenuLabel: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
    DropdownMenuSeparator: () => <hr />,
    DropdownMenuItem: ({ children, onSelect }: { children?: React.ReactNode; onSelect?: () => void }) => (
      <button role="menuitem" onClick={() => onSelect?.()}>
        {children}
      </button>
    ),
    DropdownMenuRadioGroup: ({ children, value, onValueChange }: Group & { children?: React.ReactNode }) => (
      <GroupCtx.Provider value={{ value, onValueChange }}>
        <div role="group">{children}</div>
      </GroupCtx.Provider>
    ),
    DropdownMenuRadioItem: ({ children, value }: { children?: React.ReactNode; value: string }) => {
      const g = useContext(GroupCtx)!;
      return (
        <button role="menuitemradio" aria-checked={g.value === value} onClick={() => g.onValueChange(value)}>
          {children}
        </button>
      );
    },
  };
});

beforeEach(() => {
  nav.search = {};
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function withClient(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>;
}

describe("LoginPage reasons", () => {
  it("shows the expired copy", () => {
    nav.search = { reason: "expired" };
    render(<LoginPage />);
    expect(screen.getByText("You've been signed out. Sign in to continue.")).toBeTruthy();
  });

  it("shows the failed copy", () => {
    nav.search = { reason: "failed" };
    render(<LoginPage />);
    expect(screen.getByText("GitHub sign-in didn't complete. Try again.")).toBeTruthy();
  });

  it("shows nothing for an unknown reason and never echoes it", () => {
    nav.search = { reason: "<script>alert(1)</script>" };
    const { container } = render(<LoginPage />);
    expect(container.innerHTML).not.toContain("script");
    expect(screen.queryByText(/signed out/)).toBeNull();
    expect(screen.queryByText(/didn't complete/)).toBeNull();
  });
});

describe("DeniedPage", () => {
  it("renders the login as literal text and joins orgs with or", async () => {
    nav.search = { login: "<b>mallory</b>" };
    const { container } = render(withClient(<DeniedPage />));
    expect(screen.getByText(/<b>mallory<\/b>/)).toBeTruthy();
    expect(container.querySelector("b")).toBeNull();
    await waitFor(() => expect(screen.getByText("ephematool-e2e")).toBeTruthy());
    expect(screen.getByText("acme/sre").tagName).toBe("CODE");
    expect(container.textContent).toContain("ephematool-e2e or acme/sre");
  });

  it("focuses the heading and returns to /login", async () => {
    nav.search = { login: "octo-outsider" };
    render(withClient(<DeniedPage />));
    expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Access denied" }));
    fireEvent.click(screen.getByRole("button", { name: "Try a different account" }));
    expect(nav.navigate).toHaveBeenCalledWith({ to: "/login" });
  });
});

describe("UserMenu theme", () => {
  it("persists the chosen theme", async () => {
    render(
      withClient(
        <ThemeProvider>
          <UserMenu login="octo-admin" avatarUrl="" role="admin" onSignOut={async () => {}} />
        </ThemeProvider>,
      ),
    );
    expect(screen.getByRole("button", { name: "Open user menu for octo-admin" })).toBeTruthy();
    expect(screen.getByRole("menuitemradio", { name: "System" }).getAttribute("aria-checked")).toBe("true");
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Dark" }));
    await waitFor(() => expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark"));
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(screen.getByRole("menuitemradio", { name: "Dark" }).getAttribute("aria-checked")).toBe("true");
  });
});
