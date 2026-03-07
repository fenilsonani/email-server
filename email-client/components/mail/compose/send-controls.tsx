"use client";

import { Button } from "@/components/ui/button";
import { useMailStore } from "@/lib/store";
import { Send, Trash2 } from "lucide-react";
import { toast } from "sonner";

export function SendControls({ composeId }: { composeId: string }) {
  const closeCompose = useMailStore((s) => s.closeCompose);

  const handleSend = () => {
    closeCompose(composeId);
    toast.success("Email sent", {
      action: {
        label: "Undo",
        onClick: () => {
          toast.info("Send undone");
        },
      },
    });
  };

  const handleDiscard = () => {
    closeCompose(composeId);
    toast("Draft discarded");
  };

  return (
    <div className="flex items-center justify-between border-t border-border px-4 py-2">
      <Button onClick={handleSend} size="sm" className="gap-1.5">
        <Send className="h-3.5 w-3.5" />
        Send
      </Button>
      <Button
        variant="ghost"
        size="icon"
        onClick={handleDiscard}
        className="h-7 w-7 text-muted-foreground hover:text-destructive"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </Button>
    </div>
  );
}
