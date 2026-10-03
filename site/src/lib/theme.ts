import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark";

const KEY = "opcode-theme";

function systemTheme(): Theme {
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function storedTheme(): Theme | null {
  try {
    const stored = localStorage.getItem(KEY);
    return stored === "light" || stored === "dark" ? stored : null;
  } catch {
    /* storage blocked: fall back to the system preference below */
    return null;
  }
}

/**
 * The reader's chosen theme. With no choice on record the system
 * preference is followed, live; the first toggle records a choice and
 * from then on only the toggle moves it. index.html sets the same
 * attribute from the same key before first paint, so there is no flash
 * of the wrong ground.
 */
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => storedTheme() ?? systemTheme());

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => {
      if (storedTheme()) return;
      setTheme(systemTheme());
    };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#080d17" : "#f7f5f1");
  }, [theme]);

  const toggle = useCallback(() => {
    setTheme((prev) => {
      const next: Theme = prev === "dark" ? "light" : "dark";
      try {
        localStorage.setItem(KEY, next);
      } catch {
        /* nothing to do: the theme still changes for this page view */
      }
      return next;
    });
  }, []);

  return { theme, toggle };
}
