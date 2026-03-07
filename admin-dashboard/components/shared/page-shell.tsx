"use client";

import { cn } from "@/lib/utils";

interface PageShellProps {
  title: React.ReactNode;
  description?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

export function PageShell({ title, description, actions, children, className }: PageShellProps) {
  return (
    <div className={cn("p-5 space-y-4", className)}>
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          {typeof title === "string" ? (
            <h1 className="text-[15px] font-semibold tracking-tight">{title}</h1>
          ) : (
            title
          )}
          {description && (
            <p className="text-[12px] text-muted-foreground/70 mt-0.5">{description}</p>
          )}
        </div>
        {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
      </div>
      {children}
    </div>
  );
}
