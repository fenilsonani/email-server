"use client";

import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useMailStore } from "@/lib/store";
import { Kbd } from "@/components/shared/kbd";

const shortcuts = [
  { category: "Navigation", items: [
    { keys: "J / K", desc: "Next / Previous email" },
    { keys: "G then I", desc: "Go to Inbox" },
    { keys: "G then S", desc: "Go to Sent" },
    { keys: "G then D", desc: "Go to Drafts" },
    { keys: "/", desc: "Focus search" },
    { keys: "⌘K", desc: "Command palette" },
  ]},
  { category: "Actions", items: [
    { keys: "C", desc: "Compose" },
    { keys: "R", desc: "Reply" },
    { keys: "A", desc: "Reply All" },
    { keys: "F", desc: "Forward" },
    { keys: "E", desc: "Archive" },
    { keys: "#", desc: "Trash" },
    { keys: "S", desc: "Star / Unstar" },
    { keys: "U", desc: "Mark unread" },
  ]},
  { category: "Selection", items: [
    { keys: "X", desc: "Select / Deselect" },
    { keys: "⇧X", desc: "Range select" },
    { keys: "Esc", desc: "Clear / Close" },
  ]},
];

export function ShortcutCheatsheet() {
  const open = useMailStore((s) => s.shortcutCheatsheetOpen);
  const setOpen = useMailStore((s) => s.setShortcutCheatsheetOpen);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Keyboard Shortcuts</DialogTitle>
        </DialogHeader>
        <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
          {shortcuts.map((group) => (
            <div key={group.category}>
              <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                {group.category}
              </h4>
              <div className="space-y-1.5">
                {group.items.map((item) => (
                  <div key={item.keys} className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">{item.desc}</span>
                    <Kbd>{item.keys}</Kbd>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
