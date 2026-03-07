"use client";

import { AvatarStack } from "@/components/shared/avatar-stack";
import { Kbd } from "@/components/shared/kbd";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useMailStore } from "@/lib/store";
import type { Thread } from "@/lib/types";
import {
  Archive,
  Forward,
  MailOpen,
  MoreHorizontal,
  Reply,
  ReplyAll,
  Star,
  Trash2,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useMediaQuery } from "@/hooks/use-media-query";

export function DetailHeader({ thread }: { thread: Thread }) {
  const archiveEmail = useMailStore((s) => s.archiveEmail);
  const trashEmail = useMailStore((s) => s.trashEmail);
  const starEmail = useMailStore((s) => s.starEmail);
  const markUnread = useMailStore((s) => s.markUnread);
  const openCompose = useMailStore((s) => s.openCompose);
  const isMobile = useMediaQuery("(max-width: 767px)");

  const lastEmail = thread.emails[thread.emails.length - 1];

  const handleReply = () => {
    openCompose({
      to: [lastEmail.from],
      subject: thread.subject,
      replyToId: lastEmail.id,
    });
  };

  const handleReplyAll = () => {
    const allRecipients = [
      lastEmail.from,
      ...lastEmail.to.filter((c) => c.email !== "fenil@fenilsonani.com"),
    ];
    openCompose({
      to: allRecipients,
      subject: thread.subject,
      replyToId: lastEmail.id,
    });
  };

  const handleForward = () => {
    openCompose({
      to: [],
      subject: `Fwd: ${thread.subject}`,
      body: `\n\n---------- Forwarded message ----------\nFrom: ${lastEmail.from.name}\nSubject: ${thread.subject}\n\n${lastEmail.snippet}`,
    });
  };

  if (isMobile) {
    return (
      <div className="border-b border-border px-4 py-3 shrink-0">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <h1 className="text-base font-semibold text-foreground leading-tight">
              {thread.subject}
            </h1>
            <p className="mt-1 text-xs text-muted-foreground truncate">
              {thread.participants.map((p) => p.name).join(", ")}
            </p>
          </div>
        </div>

        <div className="mt-2 flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleReply}
            className="h-9 w-9 p-0 text-muted-foreground hover:text-foreground"
          >
            <Reply className="h-3.5 w-3.5" />
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-9 w-9 p-0 text-muted-foreground hover:text-foreground"
              >
                <MoreHorizontal className="h-3.5 w-3.5" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={handleReplyAll}>
                <ReplyAll className="mr-2 h-3.5 w-3.5" />
                Reply All
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleForward}>
                <Forward className="mr-2 h-3.5 w-3.5" />
                Forward
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => archiveEmail(lastEmail.id)}>
                <Archive className="mr-2 h-3.5 w-3.5" />
                Archive
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => trashEmail(lastEmail.id)}>
                <Trash2 className="mr-2 h-3.5 w-3.5" />
                Trash
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => starEmail(lastEmail.id)}>
                <Star className="mr-2 h-3.5 w-3.5" />
                {thread.starred ? "Unstar" : "Star"}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => markUnread(lastEmail.id)}>
                <MailOpen className="mr-2 h-3.5 w-3.5" />
                Mark as unread
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    );
  }

  // Desktop layout
  const actions = [
    { icon: Reply, label: "Reply", shortcut: "R", onClick: handleReply },
    { icon: ReplyAll, label: "Reply All", shortcut: "A", onClick: handleReplyAll },
    { icon: Forward, label: "Forward", shortcut: "F", onClick: handleForward },
  ];

  const secondaryActions = [
    { icon: Archive, label: "Archive", shortcut: "E", onClick: () => archiveEmail(lastEmail.id) },
    { icon: Trash2, label: "Trash", shortcut: "#", onClick: () => trashEmail(lastEmail.id) },
    {
      icon: Star,
      label: thread.starred ? "Unstar" : "Star",
      shortcut: "S",
      onClick: () => starEmail(lastEmail.id),
    },
  ];

  return (
    <div className="border-b border-border px-4 py-3 shrink-0">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <h1 className="text-lg font-semibold text-foreground leading-tight">
            {thread.subject}
          </h1>
          <p className="mt-1 text-xs text-muted-foreground truncate">
            {thread.participants.map((p) => p.name).join(", ")}
          </p>
        </div>
      </div>

      <div className="mt-2 flex items-center gap-1">
        {actions.map((action) => (
          <Tooltip key={action.label} delayDuration={300}>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="sm" onClick={action.onClick} className="h-7 gap-1.5 px-2.5 text-muted-foreground hover:text-foreground">
                <action.icon className="h-3.5 w-3.5" />
                <span className="text-[13px]">{action.label}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              {action.label} <Kbd>{action.shortcut}</Kbd>
            </TooltipContent>
          </Tooltip>
        ))}

        <Separator orientation="vertical" className="mx-1 h-4" />

        {secondaryActions.map((action) => (
          <Tooltip key={action.label} delayDuration={300}>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon" onClick={action.onClick} className="h-7 w-7 text-muted-foreground hover:text-foreground">
                <action.icon className="h-3.5 w-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              {action.label} <Kbd>{action.shortcut}</Kbd>
            </TooltipContent>
          </Tooltip>
        ))}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-foreground">
              <MoreHorizontal className="h-3.5 w-3.5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => markUnread(lastEmail.id)}>
              <MailOpen className="mr-2 h-3.5 w-3.5" />
              Mark as unread
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
