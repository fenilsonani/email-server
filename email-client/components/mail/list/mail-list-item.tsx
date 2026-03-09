"use client";

import { ContactAvatar } from "@/components/shared/avatar-stack";
import { useMailStore } from "@/lib/store";
import type { Thread } from "@/lib/types";
import { cn } from "@/lib/utils";
import { formatDistanceToNowStrict } from "date-fns";
import { motion, useMotionValue, useTransform } from "framer-motion";
import { Archive, Check, Star, Trash2 } from "lucide-react";
import { useMediaQuery } from "@/hooks/use-media-query";

export function MailListItem({ thread, index }: { thread: Thread; index: number }) {
  const selectedEmailId = useMailStore((s) => s.selectedEmailId);
  const setSelectedEmailId = useMailStore((s) => s.setSelectedEmailId);
  const starEmail = useMailStore((s) => s.starEmail);
  const archiveEmail = useMailStore((s) => s.archiveEmail);
  const trashEmail = useMailStore((s) => s.trashEmail);
  const selectedIds = useMailStore((s) => s.selectedIds);
  const toggleSelectEmail = useMailStore((s) => s.toggleSelectEmail);

  const viewMode = useMailStore((s) => s.viewMode);
  const isMobile = useMediaQuery("(max-width: 767px)");
  const x = useMotionValue(0);
  const archiveOpacity = useTransform(x, [0, 80], [0, 1]);
  const trashOpacity = useTransform(x, [-80, 0], [1, 0]);

  const lastEmail = thread.emails[thread.emails.length - 1];
  const isSelected = thread.emails.some((e) => e.id === selectedEmailId);
  const isChecked = selectedIds.has(lastEmail.id);
  const hasSelection = selectedIds.size > 0;
  const isUnread = thread.unreadCount > 0;
  const sender = thread.emails[0].from;

  const handleDragEnd = () => {
    const currentX = x.get();
    if (currentX > 80) {
      archiveEmail(lastEmail.id);
    } else if (currentX < -80) {
      trashEmail(lastEmail.id);
    }
  };

  const handleClick = () => {
    if (hasSelection) {
      toggleSelectEmail(lastEmail.id);
    } else {
      setSelectedEmailId(lastEmail.id);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handleClick();
    }
  };

  const hoverActions = !isMobile && (
    <div
      className={cn(
        "absolute right-2 hidden items-center gap-1 rounded-lg border border-border bg-card p-1 shadow-sm group-hover:flex",
        viewMode === "list" ? "top-1/2 -translate-y-1/2" : "bottom-2"
      )}
      onClick={(e) => e.stopPropagation()}
    >
      <div
        role="button"
        tabIndex={-1}
        onClick={() => archiveEmail(lastEmail.id)}
        className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground transition-colors cursor-pointer"
      >
        <Archive className="h-3.5 w-3.5" />
      </div>
      <div
        role="button"
        tabIndex={-1}
        onClick={() => trashEmail(lastEmail.id)}
        className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-destructive transition-colors cursor-pointer"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </div>
      <div
        role="button"
        tabIndex={-1}
        onClick={() => starEmail(lastEmail.id)}
        className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-amber-400 transition-colors cursor-pointer"
      >
        <Star className="h-3.5 w-3.5" />
      </div>
    </div>
  );

  // Compact single-row layout for list view mode
  const compactContent = (
    <div
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      className={cn(
        "group relative flex w-full items-center gap-3 px-4 py-2 text-left transition-colors cursor-pointer outline-none focus-visible:ring-1 focus-visible:ring-ring h-9",
        isChecked
          ? "bg-primary/8"
          : isSelected
            ? "bg-accent/80"
            : "hover:bg-accent/40",
      )}
    >
      {/* Unread dot */}
      <div className="w-1.5 shrink-0">
        {isUnread && <div className="h-1.5 w-1.5 rounded-full bg-primary" />}
      </div>

      {/* Checkbox */}
      <div
        className="shrink-0"
        onClick={(e) => {
          e.stopPropagation();
          toggleSelectEmail(lastEmail.id);
        }}
      >
        <div
          className={cn(
            "flex h-4 w-4 items-center justify-center rounded-sm border transition-colors cursor-pointer",
            isChecked
              ? "border-primary bg-primary text-primary-foreground"
              : "border-muted-foreground/30 hover:border-muted-foreground/50"
          )}
        >
          {isChecked && <Check className="h-3 w-3" />}
        </div>
      </div>

      {/* Star */}
      <div
        className="shrink-0"
        onClick={(e) => {
          e.stopPropagation();
          starEmail(lastEmail.id);
        }}
      >
        <Star
          className={cn(
            "h-3.5 w-3.5 cursor-pointer transition-colors",
            thread.starred
              ? "fill-amber-400 text-amber-400"
              : "text-muted-foreground/40 hover:text-amber-400"
          )}
        />
      </div>

      {/* Sender — fixed width */}
      <span
        className={cn(
          "w-[180px] shrink-0 truncate text-sm",
          isUnread ? "font-semibold text-foreground" : "text-foreground/80"
        )}
      >
        {sender.name}
        {thread.emails.length > 1 && (
          <span className="ml-1 text-[11px] font-normal text-muted-foreground">
            ({thread.emails.length})
          </span>
        )}
      </span>

      {/* Subject + snippet — flexible */}
      <div className="min-w-0 flex-1 flex items-center gap-1.5 overflow-hidden">
        <span
          className={cn(
            "shrink-0 truncate text-sm max-w-[40%]",
            isUnread ? "font-semibold text-foreground" : "text-foreground/80"
          )}
        >
          {thread.subject}
        </span>
        <span className="text-sm text-muted-foreground/60">—</span>
        <span className="min-w-0 truncate text-sm text-muted-foreground/60">
          {lastEmail.snippet}
        </span>
      </div>

      {/* Date — right */}
      <span className="shrink-0 text-[11px] text-muted-foreground ml-2">
        {formatDistanceToNowStrict(new Date(thread.lastDate), { addSuffix: false })}
      </span>

      {hoverActions}
    </div>
  );

  const content = viewMode === "list" && !isMobile ? compactContent : (
    <div
      role="button"
      tabIndex={0}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
      className={cn(
        "group relative flex w-full items-start gap-3 px-4 py-3 text-left transition-colors cursor-pointer outline-none focus-visible:ring-1 focus-visible:ring-ring",
        isChecked
          ? "bg-primary/8"
          : isSelected
            ? "bg-accent/80"
            : "hover:bg-accent/40",
        isUnread && "border-l-2 border-primary",
        !isUnread && "border-l-2 border-transparent"
      )}
    >
      {/* Avatar / Checkbox */}
      <div
        className="relative mt-0.5 shrink-0"
        onClick={(e) => {
          e.stopPropagation();
          toggleSelectEmail(lastEmail.id);
        }}
      >
        <ContactAvatar
          contact={sender}
          size="md"
          className={cn(
            "transition-opacity",
            hasSelection && !isChecked ? "group-hover:opacity-100" : "",
            isChecked ? "opacity-0" : "group-hover:opacity-0"
          )}
        />
        <div
          className={cn(
            "absolute inset-0 flex items-center justify-center transition-opacity",
            isChecked ? "opacity-100" : "opacity-0 group-hover:opacity-100"
          )}
        >
          <div
            className={cn(
              "flex h-8 w-8 items-center justify-center rounded-full border-2 transition-colors cursor-pointer",
              isChecked
                ? "border-primary bg-primary text-primary-foreground"
                : "border-muted-foreground/30 bg-background hover:border-muted-foreground/50"
            )}
          >
            {isChecked && <Check className="h-3.5 w-3.5" />}
          </div>
        </div>
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              "truncate text-sm",
              isUnread ? "font-semibold text-foreground" : "font-medium text-foreground/80"
            )}
          >
            {sender.name}
          </span>
          {thread.emails.length > 1 && (
            <span className="shrink-0 text-[11px] text-muted-foreground">
              {thread.emails.length}
            </span>
          )}
          <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">
            {formatDistanceToNowStrict(new Date(thread.lastDate), { addSuffix: false })}
          </span>
        </div>

        <p
          className={cn(
            "truncate text-sm",
            isUnread ? "font-medium text-foreground/90" : "text-muted-foreground"
          )}
        >
          {thread.subject}
        </p>

        <p className="truncate text-xs text-muted-foreground/70 mt-0.5">
          {lastEmail.snippet}
        </p>
      </div>

      {/* Starred indicator */}
      {thread.starred && (
        <Star className="mt-1.5 h-3 w-3 shrink-0 fill-amber-400/80 text-amber-400/80" />
      )}

      {hoverActions}
    </div>
  );

  // On mobile: wrap with swipe gestures
  if (isMobile) {
    return (
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: Math.min(index * 0.01, 0.15), duration: 0.12 }}
        className="relative overflow-hidden"
      >
        {/* Swipe background indicators */}
        <div className="absolute inset-0 flex items-center justify-between px-6">
          <motion.div style={{ opacity: archiveOpacity }} className="flex items-center gap-2 text-green-500">
            <Archive className="h-5 w-5" />
          </motion.div>
          <motion.div style={{ opacity: trashOpacity }} className="flex items-center gap-2 text-red-500">
            <Trash2 className="h-5 w-5" />
          </motion.div>
        </div>

        <motion.div
          style={{ x }}
          drag="x"
          dragConstraints={{ left: 0, right: 0 }}
          dragElastic={0.3}
          onDragEnd={handleDragEnd}
          className="relative bg-background"
        >
          {content}
        </motion.div>
      </motion.div>
    );
  }

  // Desktop: no swipe
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ delay: Math.min(index * 0.02, 0.3), duration: 0.2 }}
    >
      {content}
    </motion.div>
  );
}
