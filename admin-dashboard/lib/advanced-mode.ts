"use client";

import { create } from "zustand";

const STORAGE_KEY = "admin-advanced-mode";

interface AdvancedModeState {
  enabled: boolean;
  hydrated: boolean;
  toggle: () => void;
  hydrate: () => void;
}

export const useAdvancedMode = create<AdvancedModeState>((set, get) => ({
  enabled: false,
  hydrated: false,

  hydrate: () => {
    if (get().hydrated) return;
    const stored = typeof window !== "undefined" && localStorage.getItem(STORAGE_KEY);
    set({ enabled: stored === "true", hydrated: true });
  },

  toggle: () => {
    const next = !get().enabled;
    localStorage.setItem(STORAGE_KEY, String(next));
    set({ enabled: next });
  },
}));
