import type { FolderSlug, Label } from "./types";

export const FOLDERS: { slug: FolderSlug; label: string; icon: string }[] = [
  { slug: "inbox", label: "Inbox", icon: "Inbox" },
  { slug: "starred", label: "Starred", icon: "Star" },
  { slug: "sent", label: "Sent", icon: "Send" },
  { slug: "drafts", label: "Drafts", icon: "FileEdit" },
  { slug: "archive", label: "Archive", icon: "Archive" },
  { slug: "trash", label: "Trash", icon: "Trash2" },
];

export const LABELS: Label[] = [
  { id: "l1", name: "Work", color: "oklch(0.65 0.2 264)" },
  { id: "l2", name: "Personal", color: "oklch(0.7 0.15 160)" },
  { id: "l3", name: "Finance", color: "oklch(0.7 0.12 45)" },
  { id: "l4", name: "Travel", color: "oklch(0.65 0.15 330)" },
  { id: "l5", name: "Newsletters", color: "oklch(0.7 0.12 85)" },
];

export const CATEGORY_COLORS: Record<string, string> = {
  primary: "oklch(0.65 0.2 264)",
  updates: "oklch(0.7 0.15 160)",
  newsletters: "oklch(0.7 0.12 45)",
  promotions: "oklch(0.65 0.15 330)",
};

export const SHORTCUTS = {
  commandPalette: { key: "k", meta: true, label: "⌘K" },
  compose: { key: "c", label: "C" },
  reply: { key: "r", label: "R" },
  replyAll: { key: "a", label: "A" },
  forward: { key: "f", label: "F" },
  archive: { key: "e", label: "E" },
  trash: { key: "#", label: "#" },
  star: { key: "s", label: "S" },
  markUnread: { key: "u", label: "U" },
  nextEmail: { key: "j", label: "J" },
  prevEmail: { key: "k", label: "K" },
  openEmail: { key: "Enter", label: "↵" },
  search: { key: "/", label: "/" },
  help: { key: "?", label: "?" },
  select: { key: "x", label: "X" },
  escape: { key: "Escape", label: "Esc" },
} as const;
