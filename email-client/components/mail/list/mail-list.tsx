"use client";

import { useMailStore } from "@/lib/store";
import { MailListItem } from "./mail-list-item";
import { MailListToolbar } from "./mail-list-toolbar";
import { BulkActionBar } from "./bulk-action-bar";
import { SplitTabs } from "./split-tabs";
import { EmptyState } from "@/components/shared/empty-state";
import { AnimatePresence } from "framer-motion";
import { Virtuoso } from "react-virtuoso";
import { FOLDERS } from "@/lib/constants";
import { Columns2, PanelLeftClose, PanelLeft, Rows3 } from "lucide-react";
import { cn } from "@/lib/utils";

export function MailList() {
  const activeFolder = useMailStore((s) => s.activeFolder);
  const activeCategory = useMailStore((s) => s.activeCategory);
  const searchQuery = useMailStore((s) => s.searchQuery);
  const selectedIds = useMailStore((s) => s.selectedIds);
  const emails = useMailStore((s) => s.emails);
  const getFilteredThreads = useMailStore((s) => s.getFilteredThreads);
  const viewMode = useMailStore((s) => s.viewMode);
  const setViewMode = useMailStore((s) => s.setViewMode);
  const sidebarCollapsed = useMailStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useMailStore((s) => s.toggleSidebar);
  const threads = getFilteredThreads();

  const folderConfig = FOLDERS.find((f) => f.slug === activeFolder);

  const sidebarToggle = (
    <button
      onClick={toggleSidebar}
      className="hidden md:flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/50 hover:bg-accent hover:text-muted-foreground transition-colors shrink-0"
      title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
    >
      {sidebarCollapsed ? (
        <PanelLeft className="h-3.5 w-3.5" />
      ) : (
        <PanelLeftClose className="h-3.5 w-3.5" />
      )}
    </button>
  );

  const viewToggle = (
    <div className="hidden md:flex items-center gap-1 shrink-0">
      <button
        onClick={() => setViewMode("split")}
        className={cn(
          "flex h-7 w-7 items-center justify-center rounded-md transition-colors",
          viewMode === "split"
            ? "bg-accent text-foreground"
            : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
        )}
        title="Split view"
      >
        <Columns2 className="h-3.5 w-3.5" />
      </button>
      <button
        onClick={() => setViewMode("list")}
        className={cn(
          "flex h-7 w-7 items-center justify-center rounded-md transition-colors",
          viewMode === "list"
            ? "bg-accent text-foreground"
            : "text-muted-foreground hover:bg-accent/50 hover:text-foreground"
        )}
        title="List view"
      >
        <Rows3 className="h-3.5 w-3.5" />
      </button>
    </div>
  );

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      {viewMode === "list" ? (
        <>
          <div className="flex items-center gap-3 border-b border-border px-4 py-3">
            {sidebarToggle}
            <h2 className="text-lg font-semibold shrink-0">{folderConfig?.label || "Inbox"}</h2>
            <div className="flex-1 min-w-0">
              <MailListToolbar inline />
            </div>
            {viewToggle}
          </div>
          <AnimatePresence mode="wait">
            {selectedIds.size > 0 && <BulkActionBar key="bulk" />}
          </AnimatePresence>
        </>
      ) : (
        <>
          <div className="flex items-center gap-3 border-b border-border px-4 py-3">
            {sidebarToggle}
            <h2 className="text-lg font-semibold">{folderConfig?.label || "Inbox"}</h2>
            <div className="flex-1" />
            {viewToggle}
          </div>
          <SplitTabs />
          <AnimatePresence mode="wait">
            {selectedIds.size > 0 ? (
              <BulkActionBar key="bulk" />
            ) : (
              <MailListToolbar key="toolbar" />
            )}
          </AnimatePresence>
        </>
      )}

      {/* Email list */}
      <div className="flex-1 overflow-hidden">
        {threads.length === 0 ? (
          <EmptyState folder={activeFolder} search={!!searchQuery} />
        ) : (
          <Virtuoso
            totalCount={threads.length}
            itemContent={(index) => (
              <MailListItem thread={threads[index]} index={index} />
            )}
            className="h-full"
          />
        )}
      </div>
    </div>
  );
}
