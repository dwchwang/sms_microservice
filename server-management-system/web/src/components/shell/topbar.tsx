"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { LogOut, Menu, X } from "lucide-react";
import { toast } from "sonner";
import { authApi } from "@/lib/api/endpoints";
import { useAuth } from "@/providers/auth-provider";
import { useAuthStore } from "@/store/auth";
import { useMonitorStatus } from "@/lib/api/hooks";
import { SidebarNav } from "./sidebar";
import { RoleBadge } from "@/components/common/status-pill";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

function MonitorDot() {
  const { data, isError } = useMonitorStatus();
  const ok = !isError && data?.status === "ok" && data?.redis_available !== false;
  return (
    <span
      title={isError ? "Monitor: không truy cập được" : `Monitor: ${data?.status ?? "..."}`}
      className="flex items-center gap-1.5 rounded-full border border-hairline px-2.5 py-1 text-xs text-body"
    >
      <span className={`size-1.5 rounded-full ${ok ? "bg-link" : "bg-mute"}`} />
      Monitor
    </span>
  );
}

export function Topbar() {
  const router = useRouter();
  const { user } = useAuth();
  const logout = useAuthStore((s) => s.logout);
  const [mobileOpen, setMobileOpen] = useState(false);

  async function handleLogout() {
    try {
      await authApi.logout();
    } catch {
      /* ignore — clear locally regardless */
    }
    logout();
    toast.success("Đã đăng xuất");
    router.replace("/login");
  }

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-hairline bg-canvas px-4 md:px-6">
      <div className="flex items-center gap-2">
        <button
          className="md:hidden"
          onClick={() => setMobileOpen((v) => !v)}
          aria-label="Menu"
        >
          {mobileOpen ? <X className="size-5" /> : <Menu className="size-5" />}
        </button>
        <span className="font-mono text-xs uppercase tracking-widest text-mute">
          VCS Server Management
        </span>
      </div>

      <div className="flex items-center gap-3">
        <MonitorDot />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="flex items-center gap-2 rounded-sm px-2 py-1 hover:bg-canvas-soft-2">
              <span className="grid size-7 place-items-center rounded-full bg-primary text-xs font-medium text-on-primary">
                {user?.username?.[0]?.toUpperCase() ?? "?"}
              </span>
              <span className="hidden text-sm text-ink sm:inline">{user?.username}</span>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>
              <div className="flex items-center justify-between gap-2">
                <span className="text-ink">{user?.full_name}</span>
                {user ? <RoleBadge role={user.role} /> : null}
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => router.push("/profile")}>Hồ sơ</DropdownMenuItem>
            <DropdownMenuItem onClick={handleLogout} className="text-error">
              <LogOut className="size-4" /> Đăng xuất
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Mobile drawer */}
      {mobileOpen ? (
        <div className="absolute left-0 right-0 top-16 z-40 border-b border-hairline bg-canvas shadow-lg md:hidden">
          <SidebarNav onNavigate={() => setMobileOpen(false)} />
        </div>
      ) : null}
    </header>
  );
}
