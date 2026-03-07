"use client";

import { create } from "zustand";
import { api } from "./api";

interface AuthState {
  authenticated: boolean;
  username: string | null;
  email: string | null;
  loading: boolean;
  checkSession: () => Promise<void>;
  login: (username: string, password: string) => Promise<{ needs_2fa: boolean }>;
  verify2FA: (code: string) => Promise<void>;
  logout: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set) => ({
  authenticated: false,
  username: null,
  email: null,
  loading: true,

  checkSession: async () => {
    try {
      const res = await api.get<{
        authenticated: boolean;
        username?: string;
        email?: string;
      }>("/auth/session");
      if (res.success && res.data?.authenticated) {
        set({
          authenticated: true,
          username: res.data.username || null,
          email: res.data.email || null,
          loading: false,
        });
      } else {
        set({ authenticated: false, username: null, email: null, loading: false });
      }
    } catch {
      set({ authenticated: false, username: null, email: null, loading: false });
    }
  },

  login: async (username: string, password: string) => {
    const res = await api.post<{ needs_2fa: boolean; username: string }>(
      "/auth/login",
      { username, password }
    );
    if (!res.success) {
      throw new Error(res.error || "Login failed");
    }
    if (!res.data?.needs_2fa) {
      set({ authenticated: true, username: res.data?.username || null, loading: false });
    }
    return { needs_2fa: res.data?.needs_2fa || false };
  },

  verify2FA: async (code: string) => {
    const res = await api.post<{ verified: boolean; username: string }>(
      "/auth/2fa",
      { code }
    );
    if (!res.success) {
      throw new Error(res.error || "Invalid 2FA code");
    }
    set({ authenticated: true, username: res.data?.username || null, loading: false });
  },

  logout: async () => {
    await api.post("/auth/logout");
    api.clearCSRF();
    set({ authenticated: false, username: null, email: null });
  },
}));
