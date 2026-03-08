"use client";

import { useTheme } from "next-themes";

export function useChartColors() {
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";

  return {
    tick: isDark ? "#a1a1aa" : "#71717a",
    grid: isDark ? "#27272a" : "#e4e4e7",
    tooltipBg: isDark ? "#18181b" : "#ffffff",
    tooltipText: isDark ? "#fafafa" : "#18181b",
    tooltipBorder: isDark ? "#27272a" : "#e4e4e7",
    pieSeparator: isDark ? "#18181b" : "#ffffff",
  };
}
