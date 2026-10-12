import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme, readThemeChoice, resolveTheme, THEME_STORAGE_KEY } from "./theme";

describe("theme", () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.classList.remove("dark");
  });

  it("resolves system against prefers-color-scheme", () => {
    expect(resolveTheme("system", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("light", true)).toBe("light");
  });

  it("defaults to system when nothing or garbage is stored", () => {
    expect(readThemeChoice()).toBe("system");
    window.localStorage.setItem(THEME_STORAGE_KEY, "purple");
    expect(readThemeChoice()).toBe("system");
  });

  it("returns stored valid values", () => {
    for (const v of ["light", "dark", "system"] as const) {
      window.localStorage.setItem(THEME_STORAGE_KEY, v);
      expect(readThemeChoice()).toBe(v);
    }
  });

  it("applyTheme toggles the dark class", () => {
    applyTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
});
