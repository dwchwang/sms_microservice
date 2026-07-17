"use client";

import { useRouter } from "next/navigation";
import { LogOut } from "lucide-react";
import { toast } from "sonner";
import { authApi } from "@/lib/api/endpoints";
import { useAuth } from "@/providers/auth-provider";
import { useAuthStore } from "@/store/auth";
import { PageHeader } from "@/components/common/page-header";
import { RoleBadge } from "@/components/common/status-pill";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDateTime } from "@/lib/utils";

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between border-b border-hairline py-3">
      <span className="text-sm text-mute">{label}</span>
      <span className="text-sm text-ink">{value}</span>
    </div>
  );
}

export default function ProfilePage() {
  const router = useRouter();
  const { user } = useAuth();
  const logout = useAuthStore((s) => s.logout);

  async function handleLogout() {
    try {
      await authApi.logout();
    } catch {
      /* ignore */
    }
    logout();
    toast.success("Đã đăng xuất");
    router.replace("/login");
  }

  if (!user) return null;

  return (
    <div className="max-w-2xl">
      <PageHeader title="Hồ sơ" description="Thông tin tài khoản & quyền hạn." />

      <Card className="p-6">
        <div className="mb-4 flex items-center gap-4">
          <span className="grid size-14 place-items-center rounded-full bg-primary text-lg font-medium text-on-primary">
            {user.full_name[0]?.toUpperCase()}
          </span>
          <div>
            <p className="display-sm text-ink">{user.full_name}</p>
            <p className="text-sm text-body">{user.email}</p>
          </div>
          <div className="ml-auto">
            <RoleBadge role={user.role} />
          </div>
        </div>

        <dl>
          <Row label="Email" value={<span className="font-mono text-xs">{user.email}</span>} />
          <Row label="Trạng thái" value={user.is_active ? "Đang hoạt động" : "Bị khoá"} />
          <Row label="Tạo lúc" value={formatDateTime(user.created_at)} />
        </dl>

        <div className="mt-4">
          <p className="mb-2 text-xs uppercase tracking-wide text-mute">Quyền hạn (scopes)</p>
          <div className="flex flex-wrap gap-1.5">
            {user.scopes.map((sc) => (
              <Badge key={sc} variant="neutral" className="font-mono">
                {sc}
              </Badge>
            ))}
          </div>
        </div>

        <div className="mt-6">
          <Button variant="secondary" onClick={handleLogout}>
            <LogOut /> Đăng xuất
          </Button>
        </div>
      </Card>
    </div>
  );
}
