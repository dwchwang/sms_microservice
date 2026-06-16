import { LayoutDashboard, Server, BarChart3, Users, User } from "lucide-react";
import type { Scope } from "@/lib/api/types";

export interface NavItem {
  href: string;
  label: string;
  icon: typeof Server;
  scope?: Scope; // required scope to show the item
}

export const NAV_ITEMS: NavItem[] = [
  { href: "/", label: "Tổng quan", icon: LayoutDashboard },
  { href: "/servers", label: "Servers", icon: Server, scope: "server:read" },
  { href: "/reports", label: "Báo cáo", icon: BarChart3, scope: "report:view" },
  { href: "/users", label: "Người dùng", icon: Users, scope: "user:manage" },
  { href: "/profile", label: "Hồ sơ", icon: User },
];
