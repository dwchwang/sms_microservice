"use client";

import { createContext, useCallback, useContext, useEffect } from "react";
import { authApi } from "@/lib/api/endpoints";
import { hasAnyScope, hasScope, tokenStorage, useAuthStore } from "@/store/auth";
import type { Scope } from "@/lib/api/types";

interface AuthContextValue {
  reload: () => Promise<void>;
  can: (scope: Scope) => boolean;
  canAny: (scopes: Scope[]) => boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { user, setUser, setReady } = useAuthStore();

  const reload = useCallback(async () => {
    if (!tokenStorage.getAccess()) {
      setUser(null);
      setReady(true);
      return;
    }
    try {
      const profile = await authApi.profile();
      setUser(profile);
    } catch {
      setUser(null);
    } finally {
      setReady(true);
    }
  }, [setUser, setReady]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const value: AuthContextValue = {
    reload,
    can: (scope) => hasScope(user, scope),
    canAny: (scopes) => hasAnyScope(user, scopes),
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  const user = useAuthStore((s) => s.user);
  const ready = useAuthStore((s) => s.ready);
  return { ...ctx, user, ready };
}
