"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, Pencil, Trash2 } from "lucide-react";
import { useServer } from "@/lib/api/hooks";
import { Can } from "@/components/common/can";
import { StatusPill } from "@/components/common/status-pill";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/common/empty-state";
import { formatDateTime, orDash } from "@/lib/utils";
import { ServerFormDialog } from "@/components/servers/server-form-dialog";
import { DeleteServerDialog } from "@/components/servers/delete-server-dialog";

function Detail({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div className="border-b border-hairline py-3">
      <dt className="text-xs uppercase tracking-wide text-mute">{label}</dt>
      <dd className={`mt-0.5 text-sm text-ink ${mono ? "font-mono" : ""}`}>{value}</dd>
    </div>
  );
}

export default function ServerDetailPage() {
  const params = useParams<{ server_id: string }>();
  const router = useRouter();
  const serverId = params.server_id;
  const { data, isLoading, isError } = useServer(serverId);
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);

  return (
    <div>
      <Button asChild variant="ghost" size="sm" className="mb-4 -ml-2">
        <Link href="/servers">
          <ArrowLeft /> Danh sách
        </Link>
      </Button>

      {isLoading ? (
        <Skeleton className="h-80 w-full" />
      ) : isError || !data ? (
        <EmptyState title="Không tìm thấy server" description={`Server "${serverId}" không tồn tại.`} />
      ) : (
        <>
          <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-3">
              <h1 className="display-md text-ink">{data.server_name}</h1>
              <StatusPill status={data.status} />
            </div>
            <div className="flex gap-2">
              <Can scope="server:update">
                <Button variant="secondary" onClick={() => setEditing(true)}>
                  <Pencil /> Sửa
                </Button>
              </Can>
              <Can scope="server:delete">
                <Button variant="destructive" onClick={() => setDeleting(true)}>
                  <Trash2 /> Xoá
                </Button>
              </Can>
            </div>
          </div>

          <Card className="p-6">
            <dl className="grid gap-x-8 sm:grid-cols-2">
              <Detail label="Server ID" value={data.server_id} mono />
              <Detail label="IPv4" value={data.ipv4} mono />
              <Detail label="Hệ điều hành" value={orDash(data.os)} />
              <Detail label="Vị trí" value={orDash(data.location)} />
              <Detail label="CPU cores" value={orDash(data.cpu_cores)} mono />
              <Detail label="RAM (GB)" value={orDash(data.ram_gb)} mono />
              <Detail label="Disk (GB)" value={orDash(data.disk_gb)} mono />
              <Detail label="Mô tả" value={orDash(data.description)} />
              <Detail label="Tạo lúc" value={formatDateTime(data.created_at)} />
              <Detail label="Cập nhật" value={formatDateTime(data.updated_at)} />
            </dl>
          </Card>

          <ServerFormDialog open={editing} onOpenChange={setEditing} server={data} />
          <DeleteServerDialog
            serverId={data.server_id}
            serverName={data.server_name}
            open={deleting}
            onOpenChange={setDeleting}
            onDeleted={() => router.replace("/servers")}
          />
        </>
      )}
    </div>
  );
}
