"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { NAV_ITEMS } from "./nav";
import { useAuth } from "@/providers/auth-provider";
import { cn } from "@/lib/utils";

export function SidebarNav({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname();
  const { can } = useAuth();

  return (
    <nav className="flex flex-col gap-1 p-3">
      {NAV_ITEMS.filter((item) => !item.scope || can(item.scope)).map((item) => {
        const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            className={cn(
              "relative flex items-center gap-3 rounded-sm px-3 py-2 text-sm transition-colors",
              active
                ? "bg-canvas-soft font-medium text-ink"
                : "text-body hover:bg-canvas-soft-2 hover:text-ink",
            )}
          >
            {active ? (
              <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-primary" />
            ) : null}
            <Icon className="size-4 shrink-0" />
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}

export function Sidebar() {
  return (
    <aside className="hidden w-60 shrink-0 border-r border-hairline bg-canvas md:block">
      <div className="sticky top-0">
        <div className="flex h-16 items-center gap-2 border-b border-hairline px-5">
          <span className="grid size-7 place-items-center rounded-sm bg-primary text-xs font-bold text-on-primary">
            VCS
          </span>
          <span className="text-sm font-medium text-ink">Server Mgmt</span>
        </div>
        <SidebarNav />
      </div>
    </aside>
  );
}
