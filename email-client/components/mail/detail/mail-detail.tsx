"use client";

import { useMailStore } from "@/lib/store";
import { ThreadView } from "./thread-view";
import { DetailHeader } from "./detail-header";
import { QuickReply } from "./quick-reply";
import { EmptyState } from "@/components/shared/empty-state";
import { motion, AnimatePresence } from "framer-motion";

export function MailDetail() {
  const selectedEmailId = useMailStore((s) => s.selectedEmailId);
  const emails = useMailStore((s) => s.emails);
  const getThreadById = useMailStore((s) => s.getThreadById);

  // Find which thread the selected email belongs to
  const selectedEmail = emails.find((e) => e.id === selectedEmailId);
  const thread = selectedEmail ? getThreadById(selectedEmail.threadId) : undefined;

  return (
    <div className="flex h-full flex-col bg-background">
      <AnimatePresence mode="wait">
        {thread ? (
          <motion.div
            key={thread.id}
            initial={{ opacity: 0, x: 12 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -12 }}
            transition={{ type: "spring", stiffness: 500, damping: 35 }}
            className="flex h-full flex-col"
          >
            <DetailHeader thread={thread} />
            <div className="flex-1 overflow-y-auto">
              <div className="mx-auto max-w-3xl px-6 py-6">
                <ThreadView thread={thread} />
                <QuickReply thread={thread} />
              </div>
            </div>
          </motion.div>
        ) : (
          <motion.div
            key="empty"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="flex h-full items-center justify-center"
          >
            <EmptyState folder="inbox" />
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
