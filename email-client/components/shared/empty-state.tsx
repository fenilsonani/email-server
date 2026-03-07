"use client";

import { motion } from "framer-motion";
import { Archive, FileEdit, Inbox, Search, Send, Star, Trash2 } from "lucide-react";
import type { FolderSlug } from "@/lib/types";

const config: Record<string, { icon: React.ElementType; title: string; description: string }> = {
  inbox: { icon: Inbox, title: "You're all caught up", description: "No new emails in this category." },
  starred: { icon: Star, title: "No starred emails", description: "Star emails to find them quickly here." },
  sent: { icon: Send, title: "No sent emails", description: "Emails you send will appear here." },
  drafts: { icon: FileEdit, title: "No drafts", description: "Your drafts will appear here." },
  archive: { icon: Archive, title: "Archive is empty", description: "Archived emails will appear here." },
  trash: { icon: Trash2, title: "Nothing in the trash", description: "Deleted emails will appear here." },
  search: { icon: Search, title: "No results", description: "Try a different search term." },
};

export function EmptyState({ folder, search }: { folder: FolderSlug; search?: boolean }) {
  const c = config[search ? "search" : folder] || config.inbox;
  const Icon = c.icon;
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4 }}
      className="flex h-full flex-col items-center justify-center gap-3 text-center px-8"
    >
      <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-muted">
        <Icon className="h-7 w-7 text-muted-foreground" />
      </div>
      <h3 className="text-base font-medium text-foreground">{c.title}</h3>
      <p className="text-sm text-muted-foreground max-w-[240px]">{c.description}</p>
    </motion.div>
  );
}
