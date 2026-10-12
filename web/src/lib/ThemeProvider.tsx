import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { applyTheme, readThemeChoice, THEME_STORAGE_KEY, type ThemeChoice } from "@/lib/theme";

type ThemeContextValue = { choice: ThemeChoice; setChoice: (c: ThemeChoice) => void };

const ThemeContext = createContext<ThemeContextValue | null>(null);

// Temporarily disables CSS transitions so switching themes never animates (UI-SPEC).
function withoutTransitions(fn: () => void): void {
  const style = document.createElement("style");
  style.appendChild(document.createTextNode("*,*::before,*::after{transition:none!important}"));
  document.head.appendChild(style);
  fn();
  // Force a style flush, then remove the override on the next frame.
  void window.getComputedStyle(document.body).opacity;
  window.requestAnimationFrame(() => style.remove());
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [choice, setChoiceState] = useState<ThemeChoice>(readThemeChoice);

  const setChoice = useCallback((c: ThemeChoice) => {
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, c);
    } catch {
      // Storage can be unavailable (private mode); the choice still applies for this page.
    }
    setChoiceState(c);
  }, []);

  useEffect(() => {
    withoutTransitions(() => applyTheme(choice));
    if (choice !== "system" || typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => withoutTransitions(() => applyTheme("system"));
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [choice]);

  const value = useMemo(() => ({ choice, setChoice }), [choice, setChoice]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside ThemeProvider");
  return ctx;
}
