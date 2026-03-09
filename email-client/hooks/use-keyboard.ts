"use client";

import { useEffect, useRef } from "react";
import { useMailStore } from "@/lib/store";

export function useKeyboard() {
  const pendingChord = useRef<string | null>(null);
  const chordTimeout = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput =
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable;
      const state = useMailStore.getState();

      // Cmd+K — command palette (works everywhere)
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        state.setCommandPaletteOpen(!state.commandPaletteOpen);
        return;
      }

      // Don't handle other shortcuts when in input
      if (isInput) return;

      // Backspace — go back to list in list view mode
      if (e.key === "Backspace") {
        if (state.viewMode === "list" && state.selectedEmailId) {
          e.preventDefault();
          state.setSelectedEmailId(null);
          return;
        }
      }

      // Escape
      if (e.key === "Escape") {
        if (state.searchOverlayOpen) {
          state.setSearchOverlayOpen(false);
        } else if (state.commandPaletteOpen) {
          state.setCommandPaletteOpen(false);
        } else if (state.shortcutCheatsheetOpen) {
          state.setShortcutCheatsheetOpen(false);
        } else if (state.selectedEmailId) {
          state.setSelectedEmailId(null);
        }
        return;
      }

      // Handle chord sequences (g then ...)
      if (pendingChord.current === "g") {
        pendingChord.current = null;
        if (chordTimeout.current) clearTimeout(chordTimeout.current);

        switch (e.key) {
          case "i":
            state.setActiveFolder("inbox");
            break;
          case "s":
            state.setActiveFolder("sent");
            break;
          case "d":
            state.setActiveFolder("drafts");
            break;
          case "a":
            state.setActiveFolder("archive");
            break;
          case "t":
            state.setActiveFolder("trash");
            break;
        }
        return;
      }

      if (e.key === "g") {
        pendingChord.current = "g";
        chordTimeout.current = setTimeout(() => {
          pendingChord.current = null;
        }, 1000);
        return;
      }

      // Single key shortcuts
      switch (e.key) {
        case "c":
          e.preventDefault();
          state.openCompose();
          break;
        case "j":
          e.preventDefault();
          state.selectNextEmail();
          break;
        case "k":
          e.preventDefault();
          state.selectPrevEmail();
          break;
        case "Enter":
        case "o":
          // Already handled by selection
          break;
        case "e":
          if (state.selectedEmailId) {
            e.preventDefault();
            state.archiveEmail(state.selectedEmailId);
          }
          break;
        case "#":
          if (state.selectedEmailId) {
            e.preventDefault();
            state.trashEmail(state.selectedEmailId);
          }
          break;
        case "s":
          if (state.selectedEmailId) {
            e.preventDefault();
            state.starEmail(state.selectedEmailId);
          }
          break;
        case "u":
          if (state.selectedEmailId) {
            e.preventDefault();
            state.markUnread(state.selectedEmailId);
          }
          break;
        case "/":
          e.preventDefault();
          if (state.commandPaletteOpen) state.setCommandPaletteOpen(false);
          state.setSearchOverlayOpen(true);
          break;
        case "?":
          e.preventDefault();
          state.setShortcutCheatsheetOpen(!state.shortcutCheatsheetOpen);
          break;
        case "r":
          if (state.selectedEmailId) {
            e.preventDefault();
            const email = state.emails.find((em) => em.id === state.selectedEmailId);
            if (email) {
              state.openCompose({
                to: [email.from],
                subject: email.subject,
                replyToId: email.id,
              });
            }
          }
          break;
        case "x":
          if (state.selectedEmailId) {
            e.preventDefault();
            if (e.shiftKey) {
              state.selectRange(state.selectedEmailId);
            } else {
              state.toggleSelectEmail(state.selectedEmailId);
            }
          }
          break;
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);
}
