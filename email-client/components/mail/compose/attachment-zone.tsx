"use client";

import { Paperclip } from "lucide-react";
import { Button } from "@/components/ui/button";

export function AttachmentZone() {
  return (
    <Button variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground">
      <Paperclip className="h-4 w-4" />
    </Button>
  );
}
