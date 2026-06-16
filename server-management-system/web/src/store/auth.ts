import { create } from "zustand";
import type { Scope, UserProfile } from "@/lib/api/types";

const ACCESS_KEY = "vcs_access_token";
const REFRESH_KEY = "vcs_refresh_token";

// NOTE: tokens kept in localStorage for an internal dashboard.
// TODO(prod): move refresh token to an httpOnly cookie to mitigate XSS.
export const tokenStorage = {
  getAccess: () => (typeof window === "undefined" ? null : localStorage.getItem(ACCESS_KEY)),
  getRefresh: () => (typeof window === "undefined" ? null : localStorage.getItem(REFRESH_KEY)),
  set: (access: string, refresh: string) => {
    localStorage.setItem(ACCESS_KEY, access);
    localStorage.setItem(REFRESH_KEY, refresh);
  },
  clear: () => {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
  },
};

interface AuthState {
  user: UserProfile | null;
  ready: boolean; // profile load attempted
  setUser: (user: UserProfile | null) => void;
  setReady: (ready: boolean) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  ready: false,
  setUser: (user) => set({ user }),
  setReady: (ready) => set({ ready }),
  logout: () => {
    tokenStorage.clear();
    set({ user: null });
  },
}));

export function hasScope(user: UserProfile | null, scope: Scope): boolean {
  return !!user?.scopes?.includes(scope);
}

export function hasAnyScope(user: UserProfile | null, scopes: Scope[]): boolean {
  return scopes.some((s) => hasScope(user, s));
}
