"use client";

import { useCallback, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { useMailStore } from "@/lib/store";
import { emails } from "@/lib/mock-data";
import { FOLDERS } from "@/lib/constants";
import { ContactAvatar } from "@/components/shared/avatar-stack";
import {
  Archive,
  FileEdit,
  Inbox,
  PenSquare,
  Search,
  Send,
  Star,
  Trash2,
} from "lucide-react";

const folderIcons: Record<string, React.ElementType> = {
  inbox: Inbox,
  starred: Star,
  sent: Send,
  drafts: FileEdit,
  archive: Archive,
  trash: Trash2,
};

interface PaletteItem {
  id: string;
  label: string;
  sublabel?: string;
  icon?: React.ElementType;
  contact?: (typeof emails)[0]["from"];
  shortcut?: string;
  onSelect: () => void;
}

export function CommandPalette() {
  const open = useMailStore((s) => s.commandPaletteOpen);
  const setOpen = useMailStore((s) => s.setCommandPaletteOpen);
  const setActiveFolder = useMailStore((s) => s.setActiveFolder);
  const setSelectedEmailId = useMailStore((s) => s.setSelectedEmailId);
  const openCompose = useMailStore((s) => s.openCompose);

  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);

  const close = useCallback(() => {
    setOpen(false);
    setQuery("");
    setSelectedIndex(0);
  }, [setOpen]);

  const actions: PaletteItem[] = [
    {
      id: "compose",
      label: "Compose new email",
      icon: PenSquare,
      shortcut: "C",
      onSelect: () => { openCompose(); close(); },
    },
    {
      id: "search",
      label: "Search emails",
      icon: Search,
      shortcut: "/",
      onSelect: () => {
        close();
        // Small delay so command palette exit animation starts before search opens
        setTimeout(() => {
          useMailStore.getState().setSearchOverlayOpen(true);
        }, 150);
      },
    },
  ];

  const folderItems: PaletteItem[] = FOLDERS.map((folder) => ({
    id: `folder-${folder.slug}`,
    label: folder.label,
    icon: folderIcons[folder.slug] || Inbox,
    onSelect: () => { setActiveFolder(folder.slug); close(); },
  }));

  const emailItems: PaletteItem[] = emails
    .filter((e) => e.folder === "inbox")
    .slice(0, 8)
    .map((email) => ({
      id: `email-${email.id}`,
      label: email.from.name,
      sublabel: email.subject,
      contact: email.from,
      onSelect: () => { setSelectedEmailId(email.id); close(); },
    }));

  const q = query.toLowerCase();
  const filterItems = (items: PaletteItem[]) =>
    q ? items.filter((i) => i.label.toLowerCase().includes(q) || i.sublabel?.toLowerCase().includes(q)) : items;

  const groups = [
    { heading: "Actions", items: filterItems(actions) },
    { heading: "Go to", items: filterItems(folderItems) },
    { heading: "Recent emails", items: filterItems(emailItems) },
  ].filter((g) => g.items.length > 0);

  const allItems = groups.flatMap((g) => g.items);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setSelectedIndex((i) => Math.min(i + 1, allItems.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setSelectedIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      allItems[selectedIndex]?.onSelect();
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) close(); else setOpen(true); }}>
      <DialogContent
        className="!top-[12vh] !translate-y-0 overflow-hidden p-0 sm:max-w-lg md:!top-[15vh]"
        showCloseButton={false}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>Command Palette</DialogTitle>
          <DialogDescription>Search for a command to run...</DialogDescription>
        </DialogHeader>

        {/* Search input */}
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(e) => { setQuery(e.target.value); setSelectedIndex(0); }}
            onKeyDown={handleKeyDown}
            placeholder="Search emails, actions, folders..."
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            autoFocus
          />
        </div>

        {/* Results */}
        <div className="max-h-[320px] overflow-y-auto py-1">
          {allItems.length === 0 && (
            <p className="py-6 text-center text-sm text-muted-foreground">No results found.</p>
          )}
          {groups.map((group) => (
            <div key={group.heading}>
              <p className="px-4 py-1.5 text-xs font-medium text-muted-foreground">
                {group.heading}
              </p>
              {group.items.map((item) => {
                const globalIdx = allItems.indexOf(item);
                const isActive = globalIdx === selectedIndex;
                const Icon = item.icon;
                return (
                  <div
                    key={item.id}
                    onClick={item.onSelect}
                    onMouseEnter={() => setSelectedIndex(globalIdx)}
                    className={`flex cursor-pointer items-center gap-3 px-4 py-2 text-sm transition-colors ${
                      isActive ? "bg-accent text-accent-foreground" : "text-foreground hover:bg-accent/50"
                    }`}
                  >
                    {item.contact ? (
                      <ContactAvatar contact={item.contact} size="sm" />
                    ) : Icon ? (
                      <Icon className="h-4 w-4 text-muted-foreground" />
                    ) : null}
                    <div className="min-w-0 flex-1">
                      <span>{item.label}</span>
                      {item.sublabel && (
                        <span className="ml-2 text-xs text-muted-foreground truncate">
                          {item.sublabel}
                        </span>
                      )}
                    </div>
                    {item.shortcut && (
                      <kbd className="rounded border border-border bg-muted px-1.5 py-0.5 text-[10px] font-mono text-muted-foreground">
                        {item.shortcut}
                      </kbd>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
