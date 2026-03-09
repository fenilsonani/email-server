"use client";

import { PenSquare } from "lucide-react";
import { useMailStore } from "@/lib/store";
import { FOLDERS } from "@/lib/constants";
import { AccountSwitcher } from "./account-switcher";
import { NavItem } from "./nav-item";
import { LabelList } from "./label-list";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export function Sidebar({ onNavigate }: { onNavigate?: () => void } = {}) {
  const collapsed = useMailStore((s) => s.sidebarCollapsed);
  const openCompose = useMailStore((s) => s.openCompose);
  const emails = useMailStore((s) => s.emails);

  const unreadCounts: Record<string, number> = {};
  for (const folder of FOLDERS) {
    if (folder.slug === "starred") {
      unreadCounts[folder.slug] = emails.filter((e) => e.starred && !e.read).length;
    } else {
      unreadCounts[folder.slug] = emails.filter((e) => e.folder === folder.slug && !e.read).length;
    }
  }

  return (
    <aside
      className={cn(
        "flex h-full flex-col border-r border-border bg-sidebar overflow-hidden shrink-0 transition-[width] duration-200 ease-out",
        collapsed ? "w-12" : "w-52"
      )}
    >
      {/* Account */}
      <div className={cn("px-2 pt-3 pb-2", collapsed && "px-1.5")}>
        <AccountSwitcher collapsed={collapsed} />
      </div>

      {/* Compose */}
      <div className={cn("px-2 mb-1", collapsed && "px-1.5")}>
        {collapsed ? (
          <Tooltip delayDuration={0}>
            <TooltipTrigger asChild>
              <button
                onClick={() => openCompose()}
                className="flex h-8 w-full items-center justify-center rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                <PenSquare className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="right">Compose</TooltipContent>
          </Tooltip>
        ) : (
          <button
            onClick={() => openCompose()}
            className="flex h-8 w-full items-center justify-center gap-2 rounded-md bg-primary text-primary-foreground text-[13px] font-medium hover:bg-primary/90 transition-colors"
          >
            <PenSquare className="h-3.5 w-3.5" />
            Compose
          </button>
        )}
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-px px-2 pt-1 overflow-y-auto">
        {FOLDERS.map((folder) => (
          <NavItem
            key={folder.slug}
            slug={folder.slug}
            label={folder.label}
            icon={folder.icon}
            badge={unreadCounts[folder.slug]}
            collapsed={collapsed}
            onNavigate={onNavigate}
          />
        ))}
        <LabelList collapsed={collapsed} />
      </nav>

      {/* Bottom spacing */}
      <div className="h-2" />
    </aside>
  );
}
