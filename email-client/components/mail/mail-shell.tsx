"use client";

import { Sidebar } from "./sidebar/sidebar";
import { MailList } from "./list/mail-list";
import { MailDetail } from "./detail/mail-detail";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ComposeModal } from "./compose/compose-modal";
import { CommandPalette } from "@/components/command/command-palette";
import { ShortcutCheatsheet } from "@/components/command/shortcut-cheatsheet";
import { MobileNav } from "./mobile-nav";
import { MobileMenu } from "./mobile-menu";
import { SearchOverlay } from "./search/search-overlay";
import { Sheet, SheetContent, SheetTitle, SheetDescription } from "@/components/ui/sheet";
import { useKeyboard } from "@/hooks/use-keyboard";
import { useMailStore } from "@/lib/store";
import { useCallback, useEffect, useRef, useState } from "react";
import { useMediaQuery } from "@/hooks/use-media-query";

export function MailShell() {
  useKeyboard();
  const composeWindows = useMailStore((s) => s.composeWindows);
  const selectedEmailId = useMailStore((s) => s.selectedEmailId);
  const setSelectedEmailId = useMailStore((s) => s.setSelectedEmailId);
  const setSidebarCollapsed = useMailStore((s) => s.setSidebarCollapsed);
  const viewMode = useMailStore((s) => s.viewMode);
  const mobileSidebarOpen = useMailStore((s) => s.mobileSidebarOpen);
  const setMobileSidebarOpen = useMailStore((s) => s.setMobileSidebarOpen);
  const searchOverlayOpen = useMailStore((s) => s.searchOverlayOpen);
  const setSearchOverlayOpen = useMailStore((s) => s.setSearchOverlayOpen);

  const isMobile = useMediaQuery("(max-width: 767px)");
  const isTablet = useMediaQuery("(min-width: 768px) and (max-width: 1023px)");
  const [showDetail, setShowDetail] = useState(false);

  // Resizable list pane
  const [listWidth, setListWidth] = useState(360);
  const isDragging = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(0);

  const onResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDragging.current = true;
    startX.current = e.clientX;
    startWidth.current = listWidth;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";

    const onMouseMove = (ev: MouseEvent) => {
      if (!isDragging.current) return;
      const delta = ev.clientX - startX.current;
      const newWidth = Math.min(Math.max(startWidth.current + delta, 280), 600);
      setListWidth(newWidth);
    };

    const onMouseUp = () => {
      isDragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    };

    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  }, [listWidth]);

  useEffect(() => {
    if (isMobile && selectedEmailId) setShowDetail(true);
    if (isMobile && !selectedEmailId) setShowDetail(false);
  }, [selectedEmailId, isMobile]);

  useEffect(() => {
    if (isTablet) setSidebarCollapsed(true);
  }, [isTablet, setSidebarCollapsed]);

  return (
    <TooltipProvider delayDuration={200}>
      <div className="flex h-screen overflow-hidden bg-background">
        {/* Desktop sidebar — hidden on mobile via CSS */}
        <div className="hidden md:flex">
          <Sidebar />
        </div>

        {/* Mobile menu bottom Sheet */}
        {isMobile && (
          <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
            <SheetContent side="bottom" showCloseButton={false} className="p-0 rounded-t-xl">
              <SheetTitle className="sr-only">Menu</SheetTitle>
              <SheetDescription className="sr-only">Navigation menu</SheetDescription>
              <MobileMenu onClose={() => setMobileSidebarOpen(false)} />
            </SheetContent>
          </Sheet>
        )}

        {/* Desktop list + detail */}
        <div className="hidden md:flex md:flex-1 md:min-w-0">
          {viewMode === "split" ? (
            <>
              <div className="shrink-0 overflow-hidden" style={{ width: listWidth }}>
                <MailList />
              </div>
              <div
                onMouseDown={onResizeStart}
                className="group relative w-0 shrink-0 cursor-col-resize"
              >
                <div className="absolute inset-y-0 -left-px w-[3px] bg-border transition-colors group-hover:bg-primary/40 group-active:bg-primary/60" />
              </div>
              <div className="flex-1 min-w-0 overflow-hidden">
                <MailDetail />
              </div>
            </>
          ) : selectedEmailId ? (
            <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
              <div className="flex items-center border-b border-border px-4 py-2 shrink-0">
                <button
                  onClick={() => setSelectedEmailId(null)}
                  className="flex h-8 items-center gap-1.5 rounded-md px-3 text-[13px] text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                >
                  ← Back to list
                </button>
              </div>
              <div className="flex-1 overflow-hidden">
                <MailDetail />
              </div>
            </div>
          ) : (
            <div className="flex-1 min-w-0 overflow-hidden">
              <MailList />
            </div>
          )}
        </div>

        {/* Mobile content */}
        <div className="flex md:hidden h-full w-full flex-col pb-14">
          {showDetail && selectedEmailId ? (
            <div className="flex h-full flex-col">
              <div className="flex items-center border-b border-border px-4 py-2 shrink-0">
                <button
                  onClick={() => {
                    setSelectedEmailId(null);
                    setShowDetail(false);
                  }}
                  className="flex h-8 items-center gap-1.5 rounded-md px-3 text-[13px] text-muted-foreground hover:bg-accent hover:text-foreground transition-colors min-h-[44px]"
                >
                  ← Back
                </button>
              </div>
              <div className="flex-1 overflow-hidden">
                <MailDetail />
              </div>
            </div>
          ) : (
            <MailList />
          )}
        </div>

        {/* Mobile bottom nav */}
        <MobileNav />

        {composeWindows.map((compose, i) => (
          <ComposeModal key={compose.id} compose={compose} index={i} />
        ))}

        <SearchOverlay open={searchOverlayOpen} onClose={() => setSearchOverlayOpen(false)} />
        <CommandPalette />
        <ShortcutCheatsheet />
      </div>
    </TooltipProvider>
  );
}
