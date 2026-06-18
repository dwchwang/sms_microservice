"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { useUsers, useUpdateUserRole } from "@/lib/api/hooks";
import { useAuth } from "@/providers/auth-provider";
import { errorMessage } from "@/lib/form";
import type { Role, UserProfile } from "@/lib/api/types";
import { PageHeader } from "@/components/common/page-header";
import { Pagination } from "@/components/common/pagination";
import { EmptyState } from "@/components/common/empty-state";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

function RoleSelect({ user, disabled }: { user: UserProfile; disabled: boolean }) {
  const update = useUpdateUserRole();
  async function onChange(role: string) {
    try {
      await update.mutateAsync({ userId: user.id, role });
      toast.success(`Đã đổi role ${user.username} → ${role}`);
    } catch (err) {
      toast.error(errorMessage(err, "Đổi role thất bại"));
    }
  }
  return (
    <Select value={user.role} onValueChange={onChange} disabled={disabled || update.isPending}>
      <SelectTrigger className="w-36">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {(["admin", "operator", "viewer"] as Role[]).map((r) => (
          <SelectItem key={r} value={r}>
            {r}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export default function UsersPage() {
  const router = useRouter();
  const { user: me, can } = useAuth();
  const [page, setPage] = useState(1);

  useEffect(() => {
    if (me && !can("user:manage")) router.replace("/403");
  }, [me, can, router]);

  const { data, isLoading, isError } = useUsers(page);
  const users = data?.items ?? [];

  return (
    <div>
      <PageHeader title="Người dùng" description="Quản lý tài khoản & phân quyền (Admin)." />

      <div className="rounded-md border border-hairline bg-canvas" style={{ boxShadow: "var(--shadow-e2)" }}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Username</TableHead>
              <TableHead>Họ tên</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Trạng thái</TableHead>
              <TableHead>Role</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 6 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: 5 }).map((__, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : users.length ? (
              users.map((u) => {
                const isSelf = u.id === me?.id;
                return (
                  <TableRow key={u.id}>
                    <TableCell className="font-medium text-ink">
                      {u.username}
                      {isSelf ? <span className="ml-2 text-xs text-mute">(bạn)</span> : null}
                    </TableCell>
                    <TableCell>{u.full_name}</TableCell>
                    <TableCell className="font-mono text-xs">{u.email}</TableCell>
                    <TableCell>
                      <Badge variant={u.is_active ? "success" : "neutral"}>
                        {u.is_active ? "Active" : "Khoá"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <RoleSelect user={u} disabled={isSelf} />
                    </TableCell>
                  </TableRow>
                );
              })
            ) : (
              <TableRow>
                <TableCell colSpan={5} className="p-0">
                  <EmptyState
                    title={isError ? "Không tải được" : "Chưa có người dùng"}
                    description={isError ? "Thử lại sau." : undefined}
                  />
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {data ? (
        <Pagination
          page={data.page}
          totalPages={data.total_pages}
          total={data.total}
          pageSize={data.page_size}
          itemLabel="người dùng"
          onChange={setPage}
        />
      ) : null}
    </div>
  );
}
