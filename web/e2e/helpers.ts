import { expect, type Page } from "@playwright/test";

// Drives the full sign-in: SPA login page -> API OAuth start -> (fake) GitHub -> callback -> "/".
export async function loginAs(page: Page, login: string): Promise<void> {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login/);
  await page.getByRole("button", { name: "Continue with GitHub" }).click();
  await page.getByRole("button", { name: login, exact: true }).click();
  await page.waitForURL((url) => url.pathname === "/");
}
