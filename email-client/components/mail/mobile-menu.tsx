"use client";

import { useMailStore } from "@/lib/store";
import { useAuthStore } from "@/lib/auth-store";
import { useRouter } from "next/navigation";
import { cn } from "@/lib/utils";
import { FOLDERS, LABELS } from "@/lib/constants";
import {
  Inbox, Star, Send, FileEdit, Archive, Trash2,
  Settings, LogOut, Moon, Sun,
} from "lucide-react";
import type { FolderSlug } from "@/lib/types";

const icons: Record<string, React.ElementType> = {
  Inbox, Star, Send, FileEdit, Archive, Trash2,
};

export function MobileMenu({ onClose }: { onClose: () => void }) {
  const router = useRouter();
  const activeFolder = useMailStore((s) => s.activeFolder);
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);
  const emails = useMailStore((s) => s.emails);
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);

  const unreadCounts: Record<string, number> = {};
  for (const folder of FOLDERS) {
    if (folder.slug === "starred") {
      unreadCounts[folder.slug] = emails.filter((e) => e.starred && !e.read).length;
    } else {
      unreadCounts[folder.slug] = emails.filter((e) => e.folder === folder.slug && !e.read).length;
    }
  }

  const handleFolder = (slug: FolderSlug) => {
    setActiveFolder(slug);
    onClose();
  };

  return (
    <div className="pb-safe">
      {/* Drag handle */}
      <div className="flex justify-center py-3">
        <div className="h-1 w-10 rounded-full bg-muted-foreground/30" />
      </div>

      {/* User info */}
      {user && (
        <div className="flex items-center gap-3 px-5 pb-4">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary/15 text-sm font-semibold text-primary">
            {user.name.charAt(0)}
          </div>
          <div className="min-w-0">
            <p className="text-sm font-medium truncate">{user.name}</p>
            <p className="text-xs text-muted-foreground truncate">{user.email}</p>
          </div>
        </div>
      )}

      <div className="h-px bg-border mx-5" />

      {/* Folders */}
      <div className="px-3 py-2">
        {FOLDERS.map((folder) => {
          const Icon = icons[folder.icon] || Inbox;
          const isActive = activeFolder === folder.slug;
          const count = unreadCounts[folder.slug];

          return (
            <button
              key={folder.slug}
              onClick={() => handleFolder(folder.slug)}
              className={cn(
                "flex w-full items-center gap-3 rounded-md px-3 py-3 text-sm transition-colors",
                isActive ? "bg-accent text-foreground font-medium" : "text-muted-foreground active:bg-accent/50"
              )}
            >
              <Icon className="h-5 w-5" />
              <span className="flex-1 text-left">{folder.label}</span>
              {count > 0 && (
                <span className="text-xs tabular-nums text-muted-foreground">{count}</span>
              )}
            </button>
          );
        })}
      </div>

      {/* Labels */}
      {LABELS.length > 0 && (
        <>
          <div className="h-px bg-border mx-5" />
          <div className="px-3 py-2">
            <p className="px-3 py-2 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/60">
              Labels
            </p>
            {LABELS.map((label) => (
              <div
                key={label.id}
                className="flex items-center gap-3 rounded-md px-3 py-2.5 text-sm text-muted-foreground"
              >
                <div className="h-2 w-2 rounded-full" style={{ backgroundColor: label.color }} />
                <span>{label.name}</span>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="h-px bg-border mx-5" />

      {/* Actions */}
      <div className="px-3 py-2">
        <button
          onClick={() => { router.push("/settings"); onClose(); }}
          className="flex w-full items-center gap-3 rounded-md px-3 py-3 text-sm text-muted-foreground active:bg-accent/50"
        >
          <Settings className="h-5 w-5" />
          Settings
        </button>
        <button
          onClick={() => { logout(); router.push("/login"); onClose(); }}
          className="flex w-full items-center gap-3 rounded-md px-3 py-3 text-sm text-red-400 active:bg-accent/50"
        >
          <LogOut className="h-5 w-5" />
          Sign out
        </button>
      </div>

      {/* Bottom safe area padding */}
      <div className="h-2" />
    </div>
  );
}
