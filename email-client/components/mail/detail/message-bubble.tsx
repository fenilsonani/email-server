"use client";

import { useState } from "react";
import type { Email } from "@/lib/types";
import { ContactAvatar } from "@/components/shared/avatar-stack";
import { formatDistanceToNowStrict, format } from "date-fns";
import { ChevronDown } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { AttachmentStrip } from "./attachment-strip";

export function MessageBubble({
  email,
  defaultExpanded = false,
}: {
  email: Email;
  defaultExpanded?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  return (
    <div className="rounded-lg border border-border bg-card overflow-hidden">
      {/* Header — always visible */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-accent/40 transition-colors"
      >
        <ContactAvatar contact={email.from} size="md" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium truncate">{email.from.name}</span>
            <span className="text-xs text-muted-foreground">
              {formatDistanceToNowStrict(new Date(email.date), { addSuffix: true })}
            </span>
          </div>
          {!expanded && (
            <p className="truncate text-xs text-muted-foreground mt-0.5">
              {email.snippet}
            </p>
          )}
        </div>
        <ChevronDown
          className={cn(
            "h-4 w-4 shrink-0 text-muted-foreground transition-transform",
            expanded && "rotate-180"
          )}
        />
      </button>

      {/* Body */}
      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ type: "spring", stiffness: 500, damping: 35 }}
          >
            <div className="border-t border-border px-4 py-1">
              <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
                <span>To: {email.to.map((c) => c.name).join(", ")}</span>
                {email.cc && email.cc.length > 0 && (
                  <span>Cc: {email.cc.map((c) => c.name).join(", ")}</span>
                )}
                <span className="ml-auto">{format(new Date(email.date), "MMM d, yyyy 'at' h:mm a")}</span>
              </div>
            </div>

            <div className="px-4 pb-4">
              <div
                className="reading-prose"
                dangerouslySetInnerHTML={{ __html: email.body }}
              />
            </div>

            {email.attachments.length > 0 && (
              <div className="border-t border-border px-4 py-3">
                <AttachmentStrip attachments={email.attachments} />
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
