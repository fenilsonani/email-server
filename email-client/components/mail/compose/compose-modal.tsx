"use client";

import { motion } from "framer-motion";
import { useMailStore } from "@/lib/store";
import type { ComposeState } from "@/lib/types";
import { ComposeEditor } from "./compose-editor";
import { RecipientInput } from "./recipient-input";
import { SendControls } from "./send-controls";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet";
import { Minus, X, Maximize2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { useMediaQuery } from "@/hooks/use-media-query";

export function ComposeModal({
  compose,
  index,
}: {
  compose: ComposeState;
  index: number;
}) {
  const closeCompose = useMailStore((s) => s.closeCompose);
  const minimizeCompose = useMailStore((s) => s.minimizeCompose);
  const updateCompose = useMailStore((s) => s.updateCompose);
  const isMobile = useMediaQuery("(max-width: 767px)");

  const offset = index * 24;

  if (compose.minimized) {
    if (isMobile) return null; // No minimize on mobile
    return (
      <motion.div
        initial={{ y: 40, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        exit={{ y: 40, opacity: 0 }}
        className="fixed z-50"
        style={{ bottom: 0, right: 80 + offset }}
      >
        <button
          onClick={() => minimizeCompose(compose.id)}
          className="flex items-center gap-2 rounded-t-lg border border-b-0 border-border bg-card px-4 py-2 text-sm font-medium shadow-lg hover:bg-accent transition-colors"
        >
          <span className="max-w-[200px] truncate">
            {compose.subject || "New Message"}
          </span>
          <Maximize2 className="h-3.5 w-3.5 text-muted-foreground" />
          <button
            onClick={(e) => {
              e.stopPropagation();
              closeCompose(compose.id);
            }}
            className="ml-1 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </button>
      </motion.div>
    );
  }

  // Mobile: bottom Sheet compose
  if (isMobile) {
    return (
      <Sheet open={!compose.minimized} onOpenChange={() => closeCompose(compose.id)}>
        <SheetContent side="bottom" showCloseButton={false} className="h-[90vh] p-0 rounded-t-xl flex flex-col">
          <SheetTitle className="sr-only">{compose.subject || "New Message"}</SheetTitle>
          <SheetDescription className="sr-only">Compose email</SheetDescription>

          {/* Title bar */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
            <span className="text-sm font-medium">
              {compose.subject || "New Message"}
            </span>
            <button
              onClick={() => closeCompose(compose.id)}
              className="flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Recipients */}
          <div className="border-b border-border px-4 py-2 space-y-1 shrink-0">
            <RecipientInput
              label="To"
              contacts={compose.to}
              onChange={(contacts) => updateCompose(compose.id, { to: contacts })}
            />
            <div className="border-t border-border pt-1">
              <Input
                placeholder="Subject"
                value={compose.subject}
                onChange={(e) => updateCompose(compose.id, { subject: e.target.value })}
                className="h-8 border-none bg-transparent px-0 text-sm focus-visible:ring-0"
              />
            </div>
          </div>

          {/* Editor */}
          <div className="flex-1 overflow-y-auto">
            <ComposeEditor
              content={compose.body}
              onChange={(body) => updateCompose(compose.id, { body })}
            />
          </div>

          {/* Send controls */}
          <SendControls composeId={compose.id} />
        </SheetContent>
      </Sheet>
    );
  }

  // Desktop: floating window
  return (
    <motion.div
      initial={{ y: 40, opacity: 0, scale: 0.95 }}
      animate={{ y: 0, opacity: 1, scale: 1 }}
      exit={{ y: 40, opacity: 0, scale: 0.95 }}
      transition={{ type: "spring", stiffness: 300, damping: 30 }}
      className="fixed z-50 flex flex-col rounded-t-xl border border-border bg-card shadow-2xl"
      style={{
        bottom: 0,
        right: 24 + offset,
        width: 520,
        height: 480,
      }}
    >
      {/* Title bar */}
      <div className="flex items-center justify-between rounded-t-xl bg-muted/50 px-4 py-2 border-b border-border">
        <span className="text-sm font-medium">
          {compose.subject || "New Message"}
        </span>
        <div className="flex items-center gap-1">
          <button
            onClick={() => minimizeCompose(compose.id)}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            <Minus className="h-3.5 w-3.5" />
          </button>
          <button
            onClick={() => closeCompose(compose.id)}
            className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Recipients */}
      <div className="border-b border-border px-4 py-2 space-y-1">
        <RecipientInput
          label="To"
          contacts={compose.to}
          onChange={(contacts) => updateCompose(compose.id, { to: contacts })}
        />
        <div className="border-t border-border pt-1">
          <Input
            placeholder="Subject"
            value={compose.subject}
            onChange={(e) => updateCompose(compose.id, { subject: e.target.value })}
            className="h-8 border-none bg-transparent px-0 text-sm focus-visible:ring-0"
          />
        </div>
      </div>

      {/* Editor */}
      <div className="flex-1 overflow-y-auto">
        <ComposeEditor
          content={compose.body}
          onChange={(body) => updateCompose(compose.id, { body })}
        />
      </div>

      {/* Send controls */}
      <SendControls composeId={compose.id} />
    </motion.div>
  );
}
