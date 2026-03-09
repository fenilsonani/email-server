"use client";

import { useTheme } from "next-themes";
import { Sun, Moon, Monitor } from "lucide-react";
import { useEffect, useState } from "react";

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  if (!mounted) return <div className="w-6 h-6" />;

  const cycle = () => {
    if (theme === "dark") setTheme("light");
    else if (theme === "light") setTheme("system");
    else setTheme("dark");
  };

  const Icon = theme === "dark" ? Moon : theme === "light" ? Sun : Monitor;
  const label = theme === "dark" ? "Dark" : theme === "light" ? "Light" : "System";

  return (
    <button
      onClick={cycle}
      className="flex items-center gap-1 rounded-md px-1.5 py-1 text-[12px] text-muted-foreground/70 transition-colors duration-100 hover:text-foreground hover:bg-accent/50"
      title={`Theme: ${label}`}
    >
      <Icon className="h-3 w-3" strokeWidth={1.5} />
      <span>{label}</span>
    </button>
  );
}
