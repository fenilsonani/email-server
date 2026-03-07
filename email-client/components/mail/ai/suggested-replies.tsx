"use client";

import { useMailStore } from "@/lib/store";
import type { Thread } from "@/lib/types";
import { motion } from "framer-motion";

export function SuggestedReplies({
  replies,
  thread,
}: {
  replies: string[];
  thread: Thread;
}) {
  const openCompose = useMailStore((s) => s.openCompose);
  const lastEmail = thread.emails[thread.emails.length - 1];

  return (
    <div className="flex flex-wrap gap-2 mt-4">
      {replies.map((reply, i) => (
        <motion.button
          key={reply}
          initial={{ opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ delay: 0.3 + i * 0.05 }}
          onClick={() =>
            openCompose({
              to: [lastEmail.from],
              subject: thread.subject,
              body: `<p>${reply}</p>`,
              replyToId: lastEmail.id,
            })
          }
          className="rounded-full border border-border bg-muted/50 px-3 py-1.5 text-xs font-medium text-foreground transition-all hover:bg-accent hover:border-primary/30 hover:-translate-y-0.5 hover:shadow-sm"
        >
          {reply}
        </motion.button>
      ))}
    </div>
  );
}
