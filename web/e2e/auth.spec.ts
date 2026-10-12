import { expect, test } from "@playwright/test";
import { loginAs } from "./helpers";

test("unauthenticated visit redirects to sign-in", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByRole("heading", { name: "Sign in to Ephematool" })).toBeVisible();
});

test("allowed member signs in and sees the status page", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  const menu = page.getByRole("menu");
  await expect(menu.getByText("octo-admin")).toBeVisible();
  await expect(menu.getByText("Admin", { exact: true })).toBeVisible();
});

test("sign out returns to sign-in", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
});

for (const login of ["octo-outsider", "octo-pending"]) {
  test(`denied member sees the denied page (${login})`, async ({ page }) => {
    await page.goto("/");
    await page.getByRole("button", { name: "Continue with GitHub" }).click();
    await page.getByRole("button", { name: login, exact: true }).click();
    await expect(page).toHaveURL(/\/denied/);
    const heading = page.getByRole("heading", { name: "Access denied" });
    await expect(heading).toBeFocused();
    await expect(page.getByText(login, { exact: false }).first()).toBeVisible();
    await expect(page.getByText("ephematool-e2e").first()).toBeVisible();
    await page.getByRole("button", { name: "Try a different account" }).click();
    await expect(page).toHaveURL(/\/login/);
  });
}

test("cancelled GitHub sign-in shows failure copy", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page).toHaveURL(/\/login/);
  await expect(page.getByText("GitHub sign-in didn't complete. Try again.")).toBeVisible();
});

test("expired session redirects with message", async ({ page, context }) => {
  test.setTimeout(60_000);
  await loginAs(page, "octo-admin");
  await expect(page.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  await context.clearCookies();
  // The 15s status poll gets a 401, which sends the SRE to /login?reason=expired.
  await expect(page).toHaveURL(/\/login\?.*reason=expired/, { timeout: 30_000 });
  await expect(page.getByText("You've been signed out. Sign in to continue.")).toBeVisible();
});

test("session survives a browser restart", async ({ page, context, browser }) => {
  await loginAs(page, "octo-admin");
  const state = await context.storageState();
  await context.close();

  const restarted = await browser.newContext({ storageState: state });
  const page2 = await restarted.newPage();
  await page2.goto("/");
  await expect(page2.getByRole("heading", { name: "Telemetry status" })).toBeVisible();
  const cookie = (await restarted.cookies()).find((c) => c.name === "ephematool_session");
  expect(cookie).toBeDefined();
  expect(cookie!.expires).toBeGreaterThan(Date.now() / 1000 + 6 * 24 * 3600);
  expect(cookie!.httpOnly).toBe(true);
  expect(cookie!.sameSite).toBe("Lax");
  await restarted.close();
});

test("user menu theme and sign out", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitemradio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
  expect(await page.evaluate(() => localStorage.getItem("ephematool-theme"))).toBe("dark");
  await page.keyboard.press("Escape");
  await page.reload();
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("button", { name: "Open user menu for octo-admin" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
});

test("viewer role badge", async ({ page }) => {
  await loginAs(page, "octo-viewer");
  await page.getByRole("button", { name: "Open user menu for octo-viewer" }).click();
  await expect(page.getByRole("menu").getByText("Viewer", { exact: true })).toBeVisible();
});
