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
  const header = page.getByRole("banner");
  await expect(header.getByText("octo-admin")).toBeVisible();
  await expect(header.getByText("Admin", { exact: true })).toBeVisible();
});

test("sign out returns to sign-in", async ({ page }) => {
  await loginAs(page, "octo-admin");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
});
