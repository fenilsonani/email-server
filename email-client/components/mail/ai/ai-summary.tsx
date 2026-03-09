"use client";

export function AiSummary({ summary }: { summary: string }) {
  return (
    <p className="text-[13px] italic text-muted-foreground/80 px-1">
      {summary}
    </p>
  );
}
