"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { AuthMethod, AuthUser } from "./types";

interface AuthState {
  isAuthenticated: boolean;
  user: AuthUser | null;
  isLoading: boolean;
  requires2FA: boolean;

  login: (method: AuthMethod) => Promise<void>;
  verifyTwoFactor: (code: string) => Promise<boolean>;
  logout: () => void;
  enable2FA: () => void;
  disable2FA: () => void;
  addPasskey: (name: string) => void;
  removePasskey: (id: string) => void;
}

const mockUser: AuthUser = {
  id: "usr_1",
  name: "Fenil Sonani",
  email: "fenil@fenilsonani.com",
  authMethod: "email",
  twoFactorEnabled: false,
  passkeys: [],
};

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      isAuthenticated: false,
      user: null,
      isLoading: false,
      requires2FA: false,

      login: async (method: AuthMethod) => {
        set({ isLoading: true });
        // Simulate network delay
        await new Promise((r) => setTimeout(r, 500));

        const user = { ...mockUser, authMethod: method };
        const currentUser = get().user;

        // Preserve 2FA and passkey settings from previous session
        if (currentUser) {
          user.twoFactorEnabled = currentUser.twoFactorEnabled;
          user.passkeys = currentUser.passkeys;
        }

        if (user.twoFactorEnabled) {
          set({ isLoading: false, requires2FA: true, user });
        } else {
          set({ isAuthenticated: true, isLoading: false, requires2FA: false, user });
        }
      },

      verifyTwoFactor: async (code: string) => {
        set({ isLoading: true });
        await new Promise((r) => setTimeout(r, 300));

        // Accept any 6-digit code
        if (/^\d{6}$/.test(code)) {
          set({ isAuthenticated: true, isLoading: false, requires2FA: false });
          return true;
        }
        set({ isLoading: false });
        return false;
      },

      logout: () => {
        set({ isAuthenticated: false, requires2FA: false });
      },

      enable2FA: () => {
        set((state) => ({
          user: state.user ? { ...state.user, twoFactorEnabled: true } : null,
        }));
      },

      disable2FA: () => {
        set((state) => ({
          user: state.user ? { ...state.user, twoFactorEnabled: false } : null,
        }));
      },

      addPasskey: (name: string) => {
        set((state) => ({
          user: state.user
            ? {
                ...state.user,
                passkeys: [
                  ...state.user.passkeys,
                  { id: `pk_${Date.now()}`, name, createdAt: new Date().toISOString() },
                ],
              }
            : null,
        }));
      },

      removePasskey: (id: string) => {
        set((state) => ({
          user: state.user
            ? { ...state.user, passkeys: state.user.passkeys.filter((p) => p.id !== id) }
            : null,
        }));
      },
    }),
    {
      name: "veil-auth",
      partialize: (state) => ({
        isAuthenticated: state.isAuthenticated,
        user: state.user,
      }),
    }
  )
);
