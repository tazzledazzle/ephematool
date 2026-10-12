export type ThemeChoice = "light" | "dark" | "system";

export const THEME_STORAGE_KEY = "ephematool-theme";

const CHOICES: readonly ThemeChoice[] = ["light", "dark", "system"];

// Only the three known values are accepted; anything else falls back to "system" (T-01-12).
export function readThemeChoice(): ThemeChoice {
  try {
    const v = window.localStorage.getItem(THEME_STORAGE_KEY);
    return CHOICES.includes(v as ThemeChoice) ? (v as ThemeChoice) : "system";
  } catch {
    return "system";
  }
}

export function resolveTheme(choice: ThemeChoice, prefersDark: boolean): "light" | "dark" {
  if (choice === "system") return prefersDark ? "dark" : "light";
  return choice;
}

export function applyTheme(choice: ThemeChoice): void {
  const prefersDark =
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches;
  document.documentElement.classList.toggle("dark", resolveTheme(choice, prefersDark) === "dark");
}
