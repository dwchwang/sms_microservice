"use client";

import { useAuth } from "@/providers/auth-provider";
import type { Scope } from "@/lib/api/types";

/** Render children only if the current user has the given scope(s). */
export function Can({
  scope,
  anyOf,
  children,
  fallback = null,
}: {
  scope?: Scope;
  anyOf?: Scope[];
  children: React.ReactNode;
  fallback?: React.ReactNode;
}) {
  const { can, canAny } = useAuth();
  const allowed = scope ? can(scope) : anyOf ? canAny(anyOf) : true;
  return <>{allowed ? children : fallback}</>;
}
