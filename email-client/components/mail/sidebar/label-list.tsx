"use client";

import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { LABELS } from "@/lib/constants";
import { cn } from "@/lib/utils";

export function LabelList({ collapsed }: { collapsed: boolean }) {
  const [open, setOpen] = useState(true);

  if (collapsed) return null;

  return (
    <div className="mt-3 pt-3 border-t border-border/50">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-1.5 px-2 py-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60 hover:text-muted-foreground transition-colors"
      >
        <ChevronRight className={cn("h-3.5 w-3.5 transition-transform duration-150", open && "rotate-90")} />
        Labels
      </button>
      {open && (
        <div className="mt-1 space-y-px">
          {LABELS.map((label) => (
            <button
              key={label.id}
              className="flex w-full items-center gap-2.5 rounded-md px-2 py-1 text-[13px] text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
            >
              <span
                className="h-2 w-2 rounded-full shrink-0"
                style={{ backgroundColor: label.color }}
              />
              <span>{label.name}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
