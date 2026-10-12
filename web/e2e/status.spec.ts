import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const GROUPS = ["Platform", "Data", "Telemetry pipeline", "Observability backends"];
const SERVICES = [
  "API",
  "Worker",
  "Dashboard",
  "Postgres",
  "Redis",
  "OTel Collector",
  "Tempo",
  "Loki",
  "Prometheus",
  "Grafana",
];

test("status page lists 10 healthy services in four fixed groups", async ({ page }) => {
  test.setTimeout(120_000);
  await loginAs(page, "octo-admin");

  const table = page.getByRole("table");
  await expect(table).toBeVisible();

  // Group headers, in order.
  const groupHeaders = table.locator("[data-group-header]");
  await expect(groupHeaders).toHaveText(GROUPS);

  // Service rows, in order (first cell of each data row).
  const names = table.locator("[data-service-row] [data-service-name]");
  await expect(names).toHaveText(SERVICES);

  // Every row converges on Healthy (the first probe cycle can lag stack start).
  const badges = table.locator("[data-service-row] [data-health]");
  await expect(badges).toHaveCount(10);
  await expect(async () => {
    for (const text of await badges.allTextContents()) expect(text.trim()).toBe("Healthy");
  }).toPass({ timeout: 60_000 });

  const signedIn = page.getByRole("region", { name: "Signed in as" });
  await expect(signedIn).toContainText("octo-admin");
  await expect(signedIn).toContainText("Admin");
  await expect(signedIn).toContainText("ephematool-e2e");

  const build = page.getByRole("region", { name: "Build" });
  await expect(build).toContainText("API");
  await expect(build).toContainText("Dashboard");
});

test("failed status request shows the trace id and retries exactly once per press", async ({
  page,
}) => {
  await loginAs(page, "octo-admin");

  let requests = 0;
  await page.route("**/api/status", (route) => {
    requests += 1;
    return route.fulfill({
      status: 500,
      headers: { "X-Trace-Id": "0123456789abcdef0123456789abcdef" },
    });
  });

  await page.reload();
  await expect(page.getByText("Couldn't load telemetry status")).toBeVisible();
  await expect(page.getByText("0123456789abcdef0123456789abcdef")).toBeVisible();
  await expect(page.getByRole("button", { name: "Copy trace ID" })).toBeVisible();

  // No silent retries: the failed load issued exactly one request.
  await page.waitForTimeout(500);
  const before = requests;
  expect(before).toBe(1);

  await page.getByRole("button", { name: "Retry Loading Status" }).click();
  await expect.poll(() => requests).toBe(before + 1);
  await page.waitForTimeout(500);
  expect(requests).toBe(before + 1);
});
