"use client";

import { useMailStore } from "@/lib/store";
import { Kbd } from "@/components/shared/kbd";
import { motion } from "framer-motion";
import { Search } from "lucide-react";

export function MailListToolbar({ inline }: { inline?: boolean }) {
  const setSearchOverlayOpen = useMailStore((s) => s.setSearchOverlayOpen);

  const searchButton = (
    <button
      onClick={() => setSearchOverlayOpen(true)}
      className="flex h-8 flex-1 items-center gap-2 rounded-lg bg-muted/50 px-3 text-[13px] text-muted-foreground/60 hover:bg-muted/80 transition-colors"
    >
      <Search className="h-3.5 w-3.5" />
      <span className="flex-1 text-left">Search emails...</span>
      <Kbd>/</Kbd>
    </button>
  );

  if (inline) {
    return searchButton;
  }

  return (
    <motion.div
      initial={{ opacity: 0, y: -4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -4 }}
      transition={{ type: "spring", stiffness: 500, damping: 30 }}
      className="flex items-center gap-2 border-b border-border px-4 py-2"
    >
      {searchButton}
    </motion.div>
  );
}
