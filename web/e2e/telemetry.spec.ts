import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

const TEMPO = "http://127.0.0.1:3200";
const WANTED = ["ephematool-dashboard", "ephematool-api", "ephematool-worker"];

type Attr = { key: string; value?: { stringValue?: string } };
type Batch = { resource?: { attributes?: Attr[] } };
type TraceDoc = { batches?: Batch[]; resourceSpans?: Batch[] };

function serviceNames(doc: TraceDoc): string[] {
  const batches = [...(doc.batches ?? []), ...(doc.resourceSpans ?? [])];
  const names = new Set<string>();
  for (const b of batches) {
    for (const a of b.resource?.attributes ?? []) {
      if (a.key === "service.name" && a.value?.stringValue) names.add(a.value.stringValue);
    }
  }
  return [...names];
}

test("one page load is one trace across dashboard, api and worker", async ({ page, request }) => {
  test.setTimeout(120_000);

  await loginAs(page, "octo-admin");

  // Reload so the page load starts with a session; capture its first status request.
  const statusRequest = page.waitForRequest((r) => new URL(r.url()).pathname === "/api/status");
  await page.reload();
  const first = await statusRequest;
  const traceparent = first.headers()["traceparent"];
  expect(traceparent).toMatch(/^00-[0-9a-f]{32}-/);
  const traceId = traceparent.split("-")[1];

  let seen: string[] = [];
  await expect
    .poll(
      async () => {
        const res = await request.get(`${TEMPO}/api/traces/${traceId}`);
        if (!res.ok()) return false;
        seen = serviceNames((await res.json()) as TraceDoc);
        return WANTED.every((s) => seen.includes(s));
      },
      {
        message: `trace ${traceId} should contain ${WANTED.join(", ")} (saw: ${seen.join(", ") || "none"})`,
        intervals: [2_000],
        timeout: 45_000,
      },
    )
    .toBe(true);

  // Under `bazel run` the cwd is the runfiles tree; hand off into the real checkout.
  const webDir = process.env.BUILD_WORKSPACE_DIRECTORY
    ? join(process.env.BUILD_WORKSPACE_DIRECTORY, "web")
    : process.cwd();
  mkdirSync(join(webDir, "test-results"), { recursive: true });
  writeFileSync(join(webDir, "test-results", "dashboard-trace-id.txt"), `${traceId}\n`);
});
