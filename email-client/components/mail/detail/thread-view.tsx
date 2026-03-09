"use client";

import type { Thread } from "@/lib/types";
import { MessageBubble } from "./message-bubble";
import { AiSummary } from "@/components/mail/ai/ai-summary";
import { SuggestedReplies } from "@/components/mail/ai/suggested-replies";
import { motion } from "framer-motion";

export function ThreadView({ thread }: { thread: Thread }) {
  const lastEmail = thread.emails[thread.emails.length - 1];

  return (
    <div className="space-y-4">
      {/* AI Summary */}
      {thread.aiSummary && <AiSummary summary={thread.aiSummary} />}

      {/* Messages */}
      {thread.emails.map((email, index) => {
        const isLast = index === thread.emails.length - 1;
        return (
          <motion.div
            key={email.id}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: index * 0.03, type: "spring", stiffness: 500, damping: 35 }}
          >
            <MessageBubble email={email} defaultExpanded={isLast} />
          </motion.div>
        );
      })}

      {/* Suggested replies */}
      {lastEmail.suggestedReplies && lastEmail.suggestedReplies.length > 0 && (
        <SuggestedReplies replies={lastEmail.suggestedReplies} thread={thread} />
      )}
    </div>
  );
}
