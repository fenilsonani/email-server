"use client";

import type { Attachment } from "@/lib/types";
import { FileText, Image, File } from "lucide-react";

function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function getIcon(type: string) {
  if (type.startsWith("image/")) return Image;
  if (type === "application/pdf") return FileText;
  return File;
}

export function AttachmentStrip({ attachments }: { attachments: Attachment[] }) {
  return (
    <div className="flex gap-2 overflow-x-auto">
      {attachments.map((a) => {
        const Icon = getIcon(a.type);
        return (
          <button
            key={a.id}
            className="flex shrink-0 items-center gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-sm transition-colors hover:bg-accent"
          >
            <Icon className="h-3.5 w-3.5 text-muted-foreground" />
            <div className="text-left">
              <p className="text-xs font-medium truncate max-w-[120px]">{a.name}</p>
              <p className="text-[10px] text-muted-foreground">{formatSize(a.size)}</p>
            </div>
          </button>
        );
      })}
    </div>
  );
}
