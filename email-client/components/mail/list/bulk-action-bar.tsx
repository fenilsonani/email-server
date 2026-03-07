"use client";

import { useMailStore } from "@/lib/store";
import { motion } from "framer-motion";
import {
  Archive,
  CheckSquare,
  Mail,
  MailOpen,
  Star,
  Trash2,
  X,
} from "lucide-react";

export function BulkActionBar() {
  const selectedIds = useMailStore((s) => s.selectedIds);
  const clearSelection = useMailStore((s) => s.clearSelection);
  const selectAll = useMailStore((s) => s.selectAll);
  const bulkArchive = useMailStore((s) => s.bulkArchive);
  const bulkTrash = useMailStore((s) => s.bulkTrash);
  const bulkStar = useMailStore((s) => s.bulkStar);
  const bulkMarkRead = useMailStore((s) => s.bulkMarkRead);
  const bulkMarkUnread = useMailStore((s) => s.bulkMarkUnread);
  const viewMode = useMailStore((s) => s.viewMode);

  const count = selectedIds.size;
  if (count === 0) return null;

  const actions = [
    { icon: Archive, label: "Archive", onClick: bulkArchive },
    { icon: Trash2, label: "Trash", onClick: bulkTrash, destructive: true },
    { icon: Star, label: "Star", onClick: bulkStar },
    { icon: MailOpen, label: "Mark read", onClick: bulkMarkRead },
    { icon: Mail, label: "Mark unread", onClick: bulkMarkUnread },
  ];

  const isListMode = viewMode === "list";

  return (
    <motion.div
      initial={{ opacity: 0, y: -4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -4 }}
      transition={{ type: "spring", stiffness: 500, damping: 30 }}
      className="flex items-center border-b border-border bg-accent/50 px-4 py-1.5 overflow-hidden shrink-0"
    >
      {/* Count + select all / deselect */}
      <div className="flex items-center gap-1.5 shrink-0">
        <button
          onClick={clearSelection}
          className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          title="Clear selection"
        >
          <X className="h-3.5 w-3.5" />
        </button>
        <span className="text-[13px] font-medium tabular-nums whitespace-nowrap">
          {count}
        </span>
        <button
          onClick={selectAll}
          className="flex h-7 items-center gap-1.5 rounded-md px-2 text-[13px] text-primary hover:bg-primary/10 transition-colors"
          title="Select all"
        >
          <CheckSquare className="h-3.5 w-3.5" />
          <span className={isListMode ? "" : "hidden"}>All</span>
        </button>
      </div>

      {/* Separator */}
      <div className="h-5 w-px bg-border mx-2 shrink-0" />

      {/* Actions */}
      <div className={`flex items-center gap-0.5 ${isListMode ? "ml-auto gap-1" : ""}`}>
        {actions.map((action) => (
          <button
            key={action.label}
            onClick={action.onClick}
            title={action.label}
            className={`flex h-7 items-center justify-center rounded-md transition-colors shrink-0 ${
              isListMode ? "gap-1.5 px-2.5" : "w-7"
            } ${
              action.destructive
                ? "text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                : "text-muted-foreground hover:bg-accent hover:text-foreground"
            }`}
          >
            <action.icon className="h-3.5 w-3.5" />
            {isListMode && <span className="text-[13px]">{action.label}</span>}
          </button>
        ))}
      </div>
    </motion.div>
  );
}
