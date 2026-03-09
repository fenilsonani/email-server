"use client";

import { cn } from "@/lib/utils";
import type { FolderSlug } from "@/lib/types";
import { useMailStore } from "@/lib/store";
import { Inbox, Star, Send, FileEdit, Archive, Trash2 } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const icons: Record<string, React.ElementType> = {
  Inbox, Star, Send, FileEdit, Archive, Trash2,
};

export function NavItem({
  slug,
  label,
  icon,
  badge,
  collapsed,
  onNavigate,
}: {
  slug: FolderSlug;
  label: string;
  icon: string;
  badge?: number;
  collapsed: boolean;
  onNavigate?: () => void;
}) {
  const activeFolder = useMailStore((s) => s.activeFolder);
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);
  const isActive = activeFolder === slug;
  const Icon = icons[icon] || Inbox;

  const content = (
    <button
      onClick={() => { setActiveFolder(slug); onNavigate?.(); }}
      className={cn(
        "group flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] transition-colors duration-100",
        isActive
          ? "bg-accent text-foreground font-medium"
          : "text-muted-foreground hover:bg-accent/50 hover:text-foreground font-normal",
        collapsed && "justify-center px-0"
      )}
    >
      <Icon
        className={cn(
          "h-4 w-4 shrink-0",
          isActive ? "text-foreground" : "text-muted-foreground/70"
        )}
        strokeWidth={isActive ? 2 : 1.5}
      />
      {!collapsed && (
        <>
          <span className="flex-1 text-left">{label}</span>
          {badge !== undefined && badge > 0 && (
            <span className="text-[11px] tabular-nums text-muted-foreground font-normal">
              {badge}
            </span>
          )}
        </>
      )}
    </button>
  );

  if (collapsed) {
    return (
      <Tooltip delayDuration={0}>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent side="right" sideOffset={8}>
          {label}{badge ? ` (${badge})` : ""}
        </TooltipContent>
      </Tooltip>
    );
  }

  return content;
}
