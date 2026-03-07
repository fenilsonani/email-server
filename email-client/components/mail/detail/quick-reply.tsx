"use client";

import { useState } from "react";
import type { Thread } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { useMailStore } from "@/lib/store";
import { Send } from "lucide-react";
import { motion } from "framer-motion";
import { toast } from "sonner";

export function QuickReply({ thread }: { thread: Thread }) {
  const [text, setText] = useState("");
  const [focused, setFocused] = useState(false);
  const openCompose = useMailStore((s) => s.openCompose);
  const lastEmail = thread.emails[thread.emails.length - 1];

  const handleSend = () => {
    if (!text.trim()) return;
    toast.success("Reply sent", {
      description: `Replied to ${lastEmail.from.name}`,
      action: { label: "Undo", onClick: () => {} },
    });
    setText("");
    setFocused(false);
  };

  const handleExpand = () => {
    openCompose({
      to: [lastEmail.from],
      subject: thread.subject,
      body: text,
      replyToId: lastEmail.id,
    });
    setText("");
    setFocused(false);
  };

  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ delay: 0.1 }}
      className="mt-4 rounded-lg border border-border bg-card p-3"
    >
      <textarea
        placeholder={`Reply to ${lastEmail.from.name}...`}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onFocus={() => setFocused(true)}
        className="w-full resize-none bg-transparent text-sm placeholder:text-muted-foreground focus:outline-none"
        rows={focused ? 4 : 1}
      />
      {focused && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          className="mt-2 flex items-center justify-between"
        >
          <button
            onClick={handleExpand}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Expand to full editor
          </button>
          <Button size="sm" onClick={handleSend} disabled={!text.trim()} className="gap-1.5">
            <Send className="h-3.5 w-3.5" />
            Send
          </Button>
        </motion.div>
      )}
    </motion.div>
  );
}
