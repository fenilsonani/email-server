import { create } from "zustand";
import type { Category, ComposeState, Contact, FolderSlug } from "./types";
import { emails, threads } from "./mock-data";

interface MailState {
  // Navigation
  activeFolder: FolderSlug;
  activeCategory: Category;
  selectedEmailId: string | null;
  selectedIds: Set<string>;

  // Search
  searchQuery: string;

  // UI
  viewMode: "split" | "list";
  sidebarCollapsed: boolean;
  mobileSidebarOpen: boolean;
  commandPaletteOpen: boolean;
  searchOverlayOpen: boolean;
  shortcutCheatsheetOpen: boolean;

  // Compose
  composeWindows: ComposeState[];

  // Data (mock)
  emails: typeof emails;
  threads: typeof threads;

  // Actions — Navigation
  setActiveFolder: (folder: FolderSlug) => void;
  setActiveCategory: (category: Category) => void;
  setSelectedEmailId: (id: string | null) => void;
  toggleSelectEmail: (id: string) => void;
  selectRange: (id: string) => void;
  clearSelection: () => void;

  // Actions — Search
  setSearchQuery: (query: string) => void;

  // Actions — UI
  setViewMode: (mode: "split" | "list") => void;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;
  setMobileSidebarOpen: (open: boolean) => void;
  setCommandPaletteOpen: (open: boolean) => void;
  setSearchOverlayOpen: (open: boolean) => void;
  setShortcutCheatsheetOpen: (open: boolean) => void;

  // Actions — Email
  archiveEmail: (id: string) => void;
  trashEmail: (id: string) => void;
  starEmail: (id: string) => void;
  markRead: (id: string) => void;
  markUnread: (id: string) => void;
  moveEmail: (id: string, folder: FolderSlug) => void;

  // Actions — Bulk
  bulkArchive: () => void;
  bulkTrash: () => void;
  bulkStar: () => void;
  bulkMarkRead: () => void;
  bulkMarkUnread: () => void;
  selectAll: () => void;

  // Actions — Compose
  openCompose: (replyTo?: { to: Contact[]; subject: string; body?: string; replyToId?: string }) => void;
  closeCompose: (id: string) => void;
  minimizeCompose: (id: string) => void;
  updateCompose: (id: string, updates: Partial<ComposeState>) => void;

  // Derived
  getFilteredThreads: () => typeof threads;
  getThreadById: (id: string) => (typeof threads)[0] | undefined;
  selectNextEmail: () => void;
  selectPrevEmail: () => void;
}

let composeCounter = 0;

export const useMailStore = create<MailState>((set, get) => ({
  activeFolder: "inbox",
  activeCategory: "primary",
  selectedEmailId: null,
  selectedIds: new Set(),
  searchQuery: "",
  viewMode: "split",
  sidebarCollapsed: false,
  mobileSidebarOpen: false,
  commandPaletteOpen: false,
  searchOverlayOpen: false,
  shortcutCheatsheetOpen: false,
  composeWindows: [],
  emails,
  threads,

  setActiveFolder: (folder) => set({ activeFolder: folder, selectedEmailId: null }),
  setActiveCategory: (category) => set({ activeCategory: category }),
  setSelectedEmailId: (id) => {
    if (id) {
      // Mark as read when selected
      set((state) => ({
        selectedEmailId: id,
        emails: state.emails.map((e) => (e.id === id ? { ...e, read: true } : e)),
      }));
    } else {
      set({ selectedEmailId: id });
    }
  },
  toggleSelectEmail: (id) =>
    set((state) => {
      const next = new Set(state.selectedIds);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return { selectedIds: next };
    }),
  selectRange: (id) =>
    set((state) => {
      const filtered = get().getFilteredThreads();
      const ids = filtered.map((t) => t.emails[t.emails.length - 1].id);
      const lastSelected = [...state.selectedIds].pop();
      if (!lastSelected) return { selectedIds: new Set([id]) };
      const startIdx = ids.indexOf(lastSelected);
      const endIdx = ids.indexOf(id);
      const [from, to] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
      const next = new Set(state.selectedIds);
      for (let i = from; i <= to; i++) next.add(ids[i]);
      return { selectedIds: next };
    }),
  clearSelection: () => set({ selectedIds: new Set() }),

  setSearchQuery: (query) => set({ searchQuery: query }),

  setViewMode: (mode) => set({ viewMode: mode }),
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setMobileSidebarOpen: (open) => set({ mobileSidebarOpen: open }),
  setCommandPaletteOpen: (open) => set({ commandPaletteOpen: open }),
  setSearchOverlayOpen: (open) => set({ searchOverlayOpen: open }),
  setShortcutCheatsheetOpen: (open) => set({ shortcutCheatsheetOpen: open }),

  archiveEmail: (id) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, folder: "archive" as FolderSlug } : e)),
      selectedEmailId: state.selectedEmailId === id ? null : state.selectedEmailId,
    })),
  trashEmail: (id) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, folder: "trash" as FolderSlug } : e)),
      selectedEmailId: state.selectedEmailId === id ? null : state.selectedEmailId,
    })),
  starEmail: (id) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, starred: !e.starred } : e)),
    })),
  markRead: (id) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, read: true } : e)),
    })),
  markUnread: (id) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, read: false } : e)),
    })),
  moveEmail: (id, folder) =>
    set((state) => ({
      emails: state.emails.map((e) => (e.id === id ? { ...e, folder } : e)),
    })),

  bulkArchive: () =>
    set((state) => ({
      emails: state.emails.map((e) =>
        state.selectedIds.has(e.id) ? { ...e, folder: "archive" as FolderSlug } : e
      ),
      selectedIds: new Set(),
      selectedEmailId: state.selectedIds.has(state.selectedEmailId || "") ? null : state.selectedEmailId,
    })),
  bulkTrash: () =>
    set((state) => ({
      emails: state.emails.map((e) =>
        state.selectedIds.has(e.id) ? { ...e, folder: "trash" as FolderSlug } : e
      ),
      selectedIds: new Set(),
      selectedEmailId: state.selectedIds.has(state.selectedEmailId || "") ? null : state.selectedEmailId,
    })),
  bulkStar: () =>
    set((state) => ({
      emails: state.emails.map((e) =>
        state.selectedIds.has(e.id) ? { ...e, starred: true } : e
      ),
    })),
  bulkMarkRead: () =>
    set((state) => ({
      emails: state.emails.map((e) =>
        state.selectedIds.has(e.id) ? { ...e, read: true } : e
      ),
    })),
  bulkMarkUnread: () =>
    set((state) => ({
      emails: state.emails.map((e) =>
        state.selectedIds.has(e.id) ? { ...e, read: false } : e
      ),
    })),
  selectAll: () => {
    const threads = get().getFilteredThreads();
    const allIds = new Set(threads.map((t) => t.emails[t.emails.length - 1].id));
    set({ selectedIds: allIds });
  },

  openCompose: (replyTo) => {
    const id = `compose-${++composeCounter}`;
    const newCompose: ComposeState = {
      id,
      to: replyTo?.to || [],
      cc: [],
      bcc: [],
      subject: replyTo?.subject ? `Re: ${replyTo.subject.replace(/^Re:\s*/i, "")}` : "",
      body: replyTo?.body || "",
      attachments: [],
      replyToId: replyTo?.replyToId,
      minimized: false,
    };
    set((state) => ({ composeWindows: [...state.composeWindows, newCompose] }));
  },
  closeCompose: (id) =>
    set((state) => ({ composeWindows: state.composeWindows.filter((c) => c.id !== id) })),
  minimizeCompose: (id) =>
    set((state) => ({
      composeWindows: state.composeWindows.map((c) =>
        c.id === id ? { ...c, minimized: !c.minimized } : c
      ),
    })),
  updateCompose: (id, updates) =>
    set((state) => ({
      composeWindows: state.composeWindows.map((c) => (c.id === id ? { ...c, ...updates } : c)),
    })),

  getFilteredThreads: () => {
    const state = get();
    // Rebuild threads from current email state
    const threadMap = new Map<string, (typeof state.emails)[0][]>();
    for (const email of state.emails) {
      const existing = threadMap.get(email.threadId) || [];
      existing.push(email);
      threadMap.set(email.threadId, existing);
    }

    const allThreads = Array.from(threadMap.entries()).map(([id, threadEmails]) => {
      const sorted = threadEmails.sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());
      const last = sorted[sorted.length - 1];
      const participants = Array.from(
        new Map(sorted.flatMap((e) => [e.from, ...e.to]).map((c) => [c.email, c])).values()
      );
      return {
        id,
        subject: sorted[0].subject.replace(/^Re:\s*/i, ""),
        emails: sorted,
        participants,
        lastDate: last.date,
        unreadCount: sorted.filter((e) => !e.read).length,
        starred: sorted.some((e) => e.starred),
        category: sorted[0].category,
        folder: last.folder,
        labels: [...new Set(sorted.flatMap((e) => e.labels))],
        aiSummary: last.aiSummary,
        aiPriority: last.aiPriority,
      };
    });

    let filtered = allThreads;

    // Filter by folder
    if (state.activeFolder === "starred") {
      filtered = filtered.filter((t) => t.starred);
    } else {
      filtered = filtered.filter((t) => t.emails.some((e) => e.folder === state.activeFolder));
    }

    // Filter by category (only for inbox, skip if "all")
    if (state.activeFolder === "inbox" && state.activeCategory !== "all") {
      filtered = filtered.filter((t) => t.category === state.activeCategory);
    }

    // Filter by search
    if (state.searchQuery) {
      const q = state.searchQuery.toLowerCase();
      filtered = filtered.filter(
        (t) =>
          t.subject.toLowerCase().includes(q) ||
          t.participants.some(
            (p) => p.name.toLowerCase().includes(q) || p.email.toLowerCase().includes(q)
          ) ||
          t.emails.some((e) => e.snippet.toLowerCase().includes(q))
      );
    }

    // Sort by last date (newest first)
    filtered.sort((a, b) => new Date(b.lastDate).getTime() - new Date(a.lastDate).getTime());

    return filtered;
  },

  getThreadById: (id) => {
    const state = get();
    const threadEmails = state.emails.filter((e) => e.threadId === id);
    if (!threadEmails.length) return undefined;
    const sorted = threadEmails.sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());
    const last = sorted[sorted.length - 1];
    const participants = Array.from(
      new Map(sorted.flatMap((e) => [e.from, ...e.to]).map((c) => [c.email, c])).values()
    );
    return {
      id,
      subject: sorted[0].subject.replace(/^Re:\s*/i, ""),
      emails: sorted,
      participants,
      lastDate: last.date,
      unreadCount: sorted.filter((e) => !e.read).length,
      starred: sorted.some((e) => e.starred),
      category: sorted[0].category,
      folder: last.folder,
      labels: [...new Set(sorted.flatMap((e) => e.labels))],
      aiSummary: last.aiSummary,
      aiPriority: last.aiPriority,
    };
  },

  selectNextEmail: () => {
    const state = get();
    const filtered = state.getFilteredThreads();
    if (!filtered.length) return;
    const currentIdx = filtered.findIndex((t) => t.emails.some((e) => e.id === state.selectedEmailId));
    const nextIdx = currentIdx < filtered.length - 1 ? currentIdx + 1 : currentIdx;
    const nextThread = filtered[nextIdx];
    const lastEmail = nextThread.emails[nextThread.emails.length - 1];
    set({ selectedEmailId: lastEmail.id });
  },

  selectPrevEmail: () => {
    const state = get();
    const filtered = state.getFilteredThreads();
    if (!filtered.length) return;
    const currentIdx = filtered.findIndex((t) => t.emails.some((e) => e.id === state.selectedEmailId));
    const prevIdx = currentIdx > 0 ? currentIdx - 1 : 0;
    const prevThread = filtered[prevIdx];
    const lastEmail = prevThread.emails[prevThread.emails.length - 1];
    set({ selectedEmailId: lastEmail.id });
  },
}));
