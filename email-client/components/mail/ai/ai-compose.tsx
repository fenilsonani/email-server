"use client";

import { Button } from "@/components/ui/button";
import { Sparkles } from "lucide-react";

const aiResponses = [
  "Thank you for your email. I've reviewed the details and everything looks good. I'll proceed with the next steps and keep you updated on the progress.",
  "I appreciate you sharing this. Let me take some time to review it thoroughly and I'll get back to you with my thoughts by end of day.",
  "Thanks for the heads up! I'll look into this right away and follow up with any questions I might have.",
];

export function AiCompose({ onInsert }: { onInsert: (text: string) => void }) {
  const handleDraft = () => {
    const response = aiResponses[Math.floor(Math.random() * aiResponses.length)];
    onInsert(`<p>${response}</p>`);
  };

  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={handleDraft}
      className="gap-1.5 text-xs text-muted-foreground hover:text-primary"
    >
      <Sparkles className="h-3.5 w-3.5" />
      Draft with AI
    </Button>
  );
}
