"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { LogOut, Menu, X } from "lucide-react";
import { toast } from "sonner";
import { authApi } from "@/lib/api/endpoints";
import { useAuth } from "@/providers/auth-provider";
import { useAuthStore } from "@/store/auth";
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
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="flex items-center gap-2 rounded-sm px-2 py-1 hover:bg-canvas-soft-2">
              <span className="grid size-7 place-items-center rounded-full bg-primary text-xs font-medium text-on-primary">
                {user?.full_name?.[0]?.toUpperCase() ?? "?"}
              </span>
              <span className="hidden text-sm text-ink sm:inline">{user?.email}</span>
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
