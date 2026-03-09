import { cn } from "@/lib/utils";

export function PriorityIndicator({ priority }: { priority?: "urgent" | "action" | "info" }) {
  if (!priority || priority === "info") return null;
  return (
    <span
      className={cn(
        "h-2 w-2 rounded-full shrink-0",
        priority === "urgent" && "bg-destructive",
        priority === "action" && "bg-amber-400"
      )}
    />
  );
}
