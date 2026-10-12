import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/schema";
import { HealthBadge } from "./HealthBadge";
import { ServiceTable } from "./ServiceTable";
import { StatusError } from "./StatusError";
import { relativeTime } from "./relativeTime";

type Service = components["schemas"]["ServiceStatus"];

// Radix tooltips load a second copy of React under Bazel's symlinked node_modules
// ("Invalid hook call"), so unit tests stub the tooltip primitives. The real tooltip is
// exercised by the Playwright status spec.
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <span data-testid="tooltip">{children}</span>
  ),
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const SPEC: Array<[string, string, Service["group"]]> = [
  ["api", "API", "platform"],
  ["worker", "Worker", "platform"],
  ["dashboard", "Dashboard", "platform"],
  ["postgres", "Postgres", "data"],
  ["redis", "Redis", "data"],
  ["otel-collector", "OTel Collector", "pipeline"],
  ["tempo", "Tempo", "backends"],
  ["loki", "Loki", "backends"],
  ["prometheus", "Prometheus", "backends"],
  ["grafana", "Grafana", "backends"],
];

function services(): Service[] {
  return SPEC.map(([id, name, group]) => ({
    id,
    name,
    group,
    status: "healthy",
    lastSignalAt: "2026-10-12T12:00:00Z",
    detail: "",
  }));
}

describe("relativeTime", () => {
  const now = Date.parse("2026-10-12T12:00:00Z");
  it("formats seconds", () => {
    expect(relativeTime(new Date(now - 12_000).toISOString(), now)).toBe("12s ago");
  });
  it("formats minutes", () => {
    expect(relativeTime(new Date(now - 3 * 60_000).toISOString(), now)).toBe("3m ago");
  });
  it("renders a dash for no signal", () => {
    expect(relativeTime(null, now)).toBe("—");
    expect(relativeTime(undefined, now)).toBe("—");
  });
});

describe("HealthBadge", () => {
  it.each([
    ["healthy", "Healthy"],
    ["degraded", "Degraded"],
    ["down", "Down"],
    ["unknown", "Unknown"],
  ] as const)("renders icon and text for %s", (status, label) => {
    const { container } = render(<HealthBadge status={status} />);
    expect(screen.getByText(label)).toBeTruthy();
    expect(container.querySelector("svg")).toBeTruthy();
  });
});

describe("ServiceTable", () => {
  it("renders four group headers in fixed order and ten rows", () => {
    const { container } = render(<ServiceTable services={services()} now={Date.now()} />);
    const headers = [...container.querySelectorAll("[data-group-header]")].map((e) =>
      e.textContent?.trim(),
    );
    expect(headers).toEqual(["Platform", "Data", "Telemetry pipeline", "Observability backends"]);
    const names = [...container.querySelectorAll("[data-service-row] [data-service-name]")].map(
      (e) => e.textContent?.trim(),
    );
    expect(names).toEqual(SPEC.map(([, name]) => name));
    // thead plus one body per group.
    expect(screen.getAllByRole("rowgroup")).toHaveLength(5);
  });

  it("shows the absolute timestamp in the title of the last signal", () => {
    const { container } = render(<ServiceTable services={services()} now={Date.parse("2026-10-12T12:00:12Z")} />);
    const cell = container.querySelector("[data-service-row] [data-last-signal]");
    expect(cell?.textContent).toBe("12s ago");
    expect(cell?.getAttribute("title")).toBe("2026-10-12T12:00:00Z");
  });

  it("renders the empty state for an empty list", () => {
    render(<ServiceTable services={[]} now={Date.now()} />);
    expect(screen.getByText("No service data yet")).toBeTruthy();
    expect(
      screen.getByText(
        "The API has not received telemetry from any service. Check that the OTel Collector is running, then select Retry Loading Status.",
      ),
    ).toBeTruthy();
  });
});

describe("StatusError", () => {
  const TRACE = "0123456789abcdef0123456789abcdef";

  it("shows the heading, the trace id in mono and a retry button", () => {
    const onRetry = vi.fn();
    render(<StatusError traceId={TRACE} onRetry={onRetry} />);
    expect(screen.getByText("Couldn't load telemetry status")).toBeTruthy();
    const id = screen.getByText(TRACE);
    expect(id.className).toContain("font-mono");
    fireEvent.click(screen.getByRole("button", { name: "Retry Loading Status" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("copies the trace id and announces it for two seconds", async () => {
    vi.useFakeTimers();
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const { container } = render(<StatusError traceId={TRACE} onRetry={() => {}} />);
    const live = container.querySelector('[aria-live="polite"]') as HTMLElement;
    expect(live).toBeTruthy();
    expect(live.textContent).toBe("");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy trace ID" }));
    });
    expect(writeText).toHaveBeenCalledWith(TRACE);
    expect(live.textContent).toBe("Trace ID copied");

    await act(async () => {
      vi.advanceTimersByTime(2000);
    });
    expect(live.textContent).toBe("");
  });

  it("omits the copy button when there is no trace id", () => {
    render(<StatusError traceId={null} onRetry={() => {}} />);
    expect(screen.queryByRole("button", { name: "Copy trace ID" })).toBeNull();
  });
});
