"use client";

import { useMailStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { Inbox, Star, PenSquare, Send, Menu } from "lucide-react";
import type { FolderSlug } from "@/lib/types";

export function MobileNav() {
  const activeFolder = useMailStore((s) => s.activeFolder);
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);
  const openCompose = useMailStore((s) => s.openCompose);
  const setMobileSidebarOpen = useMailStore((s) => s.setMobileSidebarOpen);

  const items: { icon: typeof Inbox; label: string; folder?: FolderSlug; action?: () => void; fab?: boolean }[] = [
    { icon: Inbox, label: "Inbox", folder: "inbox" },
    { icon: Star, label: "Starred", folder: "starred" },
    { icon: PenSquare, label: "Compose", action: () => openCompose(), fab: true },
    { icon: Send, label: "Sent", folder: "sent" },
    { icon: Menu, label: "Menu", action: () => setMobileSidebarOpen(true) },
  ];

  return (
    <nav className="fixed bottom-0 left-0 right-0 z-40 flex h-14 items-center justify-around border-t border-border bg-background/95 backdrop-blur-sm md:hidden">
      {items.map((item) => {
        const isActive = item.folder ? activeFolder === item.folder : false;

        if (item.fab) {
          return (
            <button
              key={item.label}
              onClick={item.action}
              className="flex h-11 w-11 items-center justify-center rounded-full bg-indigo-600 text-white shadow-lg active:scale-95 transition-transform"
            >
              <item.icon className="h-5 w-5" />
            </button>
          );
        }

        return (
          <button
            key={item.label}
            onClick={() => {
              if (item.folder) setActiveFolder(item.folder);
              if (item.action) item.action();
            }}
            className={cn(
              "flex min-h-[44px] min-w-[44px] flex-col items-center justify-center gap-0.5 text-[10px]",
              isActive ? "text-primary" : "text-muted-foreground"
            )}
          >
            <item.icon className="h-5 w-5" />
            <span>{item.label}</span>
          </button>
        );
      })}
    </nav>
  );
}
