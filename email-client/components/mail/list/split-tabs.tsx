"use client";

import { useMailStore } from "@/lib/store";
import type { Category } from "@/lib/types";
import { cn } from "@/lib/utils";
import { motion } from "framer-motion";

const categories: { value: Category; label: string; color: string }[] = [
  { value: "all", label: "All", color: "var(--cat-primary)" },
  { value: "primary", label: "Primary", color: "var(--cat-primary)" },
  { value: "updates", label: "Updates", color: "var(--cat-updates)" },
  { value: "newsletters", label: "Newsletters", color: "var(--cat-newsletters)" },
  { value: "promotions", label: "Promotions", color: "var(--cat-promotions)" },
];

export function SplitTabs() {
  const activeCategory = useMailStore((s) => s.activeCategory);
  const setActiveCategory = useMailStore((s) => s.setActiveCategory);
  const activeFolder = useMailStore((s) => s.activeFolder);
  const emails = useMailStore((s) => s.emails);

  if (activeFolder !== "inbox") return null;

  return (
    <div className="relative flex overflow-x-auto border-b border-border scrollbar-none">
      {categories.map((cat) => {
        const unread =
          cat.value === "all"
            ? emails.filter((e) => e.folder === "inbox" && !e.read).length
            : emails.filter(
                (e) => e.folder === "inbox" && e.category === cat.value && !e.read
              ).length;
        const isActive = activeCategory === cat.value;
        return (
          <button
            key={cat.value}
            onClick={() => setActiveCategory(cat.value)}
            className={cn(
              "relative shrink-0 px-4 py-2.5 text-[13px] font-medium transition-colors whitespace-nowrap",
              isActive ? "text-foreground" : "text-muted-foreground hover:text-foreground"
            )}
          >
            <span className="flex items-center justify-center gap-1.5">
              {cat.label}
              {unread > 0 && (
                <span
                  className="min-w-[16px] rounded-full px-1 py-0 text-[10px] font-semibold text-white"
                  style={{ backgroundColor: cat.color }}
                >
                  {unread}
                </span>
              )}
            </span>
            {isActive && (
              <motion.div
                layoutId="category-tab"
                className="absolute -bottom-px left-0 right-0 z-10 h-[2px]"
                style={{ backgroundColor: cat.color }}
                transition={{ type: "spring", stiffness: 500, damping: 35 }}
              />
            )}
          </button>
        );
      })}
    </div>
  );
}
