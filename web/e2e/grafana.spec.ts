import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

// Grafana is published on the host, not behind nginx, so it uses an absolute URL (not baseURL).
const GRAFANA = "http://127.0.0.1:3000";

test("provisioned platform dashboard loads", async ({ page }) => {
  await page.goto(`${GRAFANA}/d/ephematool-platform`);
  await expect(page.getByText("Request rate").first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("p95 latency").first()).toBeVisible();
  await expect(page.getByText("Worker tasks").first()).toBeVisible();
  await expect(page.getByText("Auth outcomes").first()).toBeVisible();
});

test("Loki log line links to its Tempo trace and the trace links back to logs (A5)", async ({
  page,
}) => {
  test.setTimeout(180_000);

  // Produce api logs that carry a trace_id.
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("table")).toBeVisible();
  await page.waitForTimeout(3_000);

  const panes = {
    a: {
      datasource: "loki",
      queries: [
        {
          refId: "A",
          expr: '{service_name="ephematool-api"} | trace_id != ""',
          queryType: "range",
          datasource: { type: "loki", uid: "loki" },
        },
      ],
      range: { from: "now-15m", to: "now" },
    },
  };
  const url = `${GRAFANA}/explore?schemaVersion=1&orgId=1&panes=${encodeURIComponent(
    JSON.stringify(panes),
  )}`;
  await page.goto(url);

  // Expand the first log row (Loki can lag a few seconds behind the collector).
  const row = page.locator('[role="row"], [data-testid="logRows"] > div').filter({
    hasText: /ephematool|auth|status|request/i,
  });
  await expect(row.first()).toBeVisible({ timeout: 60_000 });
  await row.first().click();

  // The trace_id derived field exposes a link/button into the Tempo datasource.
  const tempoLink = page.getByRole("button", { name: /^Tempo$/ }).or(
    page.getByRole("link", { name: /^Tempo$/ }),
  );
  await expect(tempoLink.first()).toBeVisible({ timeout: 30_000 });
  await tempoLink.first().click();

  // The trace view opens in the split pane and shows the api service.
  const traceView = page.getByText("Trace ID").first();
  await expect(traceView).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("ephematool/ephematool-api").first()).toBeVisible();

  // Expand the root span, then follow the tracesToLogsV2 link ("Logs for this trace" in
  // Grafana 13, "Logs for this span" in older builds) back to Loki.
  // The root span row is the last match for the service-and-operation label.
  await page.getByText("ephematool/ephematool-api").last().click();
  const logsLink = page
    .getByRole("button", { name: /Logs for this (trace|span)/i })
    .or(page.getByRole("link", { name: /Logs for this (trace|span)/i }));
  await expect(logsLink.first()).toBeVisible({ timeout: 30_000 });
  await logsLink.first().click();

  // The link opens a Loki query scoped to this trace.
  await expect(page.getByText(/Logs volume/i).first()).toBeVisible({ timeout: 30_000 });
});
