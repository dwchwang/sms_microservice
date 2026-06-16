"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Plus, Upload, Search, ArrowUp, ArrowDown, Pencil, Trash2, Eye, X } from "lucide-react";
import { useServers } from "@/lib/api/hooks";
import type { ServerListParams } from "@/lib/api/endpoints";
import type { ServerResponse } from "@/lib/api/types";
import { Can } from "@/components/common/can";
import { PageHeader } from "@/components/common/page-header";
import { StatusPill } from "@/components/common/status-pill";
import { EmptyState } from "@/components/common/empty-state";
import { Pagination } from "@/components/common/pagination";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { orDash } from "@/lib/utils";
import { ServerFormDialog } from "@/components/servers/server-form-dialog";
import { DeleteServerDialog } from "@/components/servers/delete-server-dialog";
import { ImportDialog } from "@/components/servers/import-dialog";
import { ExportButton } from "@/components/servers/export-button";

const SORTABLE = [
  ["server_id", "Server ID"],
  ["server_name", "Tên"],
  ["status", "Trạng thái"],
  ["ipv4", "IPv4"],
  ["location", "Vị trí"],
  ["created_at", "Tạo lúc"],
] as const;

function ServersView() {
  const router = useRouter();
  const sp = useSearchParams();

  const [search, setSearch] = useState(sp.get("server_name") ?? "");
  const [importOpen, setImportOpen] = useState(sp.get("import") === "1");
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ServerResponse | null>(null);
  const [deleting, setDeleting] = useState<ServerResponse | null>(null);

  const status = (sp.get("status") as "on" | "off" | null) ?? undefined;
  const sortBy = sp.get("sort_by") ?? "created_at";
  const sortOrder = (sp.get("sort_order") as "asc" | "desc") ?? "desc";
  const page = Number(sp.get("page") ?? "1");
  const serverName = sp.get("server_name") ?? undefined;

  const params: ServerListParams = useMemo(
    () => ({
      page,
      page_size: 20,
      status,
      server_name: serverName,
      sort_by: sortBy,
      sort_order: sortOrder,
    }),
    [page, status, serverName, sortBy, sortOrder],
  );

  const { data, isLoading, isError } = useServers(params);

  function setParam(updates: Record<string, string | undefined>) {
    const next = new URLSearchParams(sp.toString());
    next.delete("import");
    for (const [k, v] of Object.entries(updates)) {
      if (v === undefined || v === "") next.delete(k);
      else next.set(k, v);
    }
    router.replace(`/servers?${next.toString()}`);
  }

  function toggleSort(col: string) {
    if (sortBy === col) {
      setParam({ sort_order: sortOrder === "asc" ? "desc" : "asc", page: "1" });
    } else {
      setParam({ sort_by: col, sort_order: "asc", page: "1" });
    }
  }

  function submitSearch(e: React.FormEvent) {
    e.preventDefault();
    setParam({ server_name: search || undefined, page: "1" });
  }

  // Xoá ô tìm kiếm và tải lại toàn bộ server (reset mọi filter tên).
  function clearSearch() {
    setSearch("");
    setParam({ server_name: undefined, page: "1" });
  }

  const servers = data?.servers ?? [];
  const hasSearch = search.length > 0 || !!serverName;

  return (
    <div>
      <PageHeader
        title="Servers"
        description="Quản lý danh sách server, trạng thái On/Off cập nhật mỗi 60s."
        actions={
          <>
            <Can scope="server:export">
              <ExportButton params={params} />
            </Can>
            <Can scope="server:import">
              <Button variant="secondary" onClick={() => setImportOpen(true)}>
                <Upload /> Import
              </Button>
            </Can>
            <Can scope="server:create">
              <Button onClick={() => setCreateOpen(true)}>
                <Plus /> Tạo server
              </Button>
            </Can>
          </>
        }
      />

      {/* Toolbar */}
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <form onSubmit={submitSearch} className="relative max-w-xs flex-1">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-mute" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Tìm theo tên server..."
            className={hasSearch ? "px-9" : "pl-9"}
          />
          {hasSearch ? (
            <button
              type="button"
              onClick={clearSearch}
              title="Xoá tìm kiếm & tải lại toàn bộ"
              aria-label="Xoá tìm kiếm"
              className="absolute right-2 top-1/2 grid size-6 -translate-y-1/2 place-items-center rounded-full text-mute hover:bg-canvas-soft-2 hover:text-ink"
            >
              <X className="size-4" />
            </button>
          ) : null}
        </form>
        <Tabs
          value={status ?? "all"}
          onValueChange={(v) => setParam({ status: v === "all" ? undefined : v, page: "1" })}
        >
          <TabsList>
            <TabsTrigger value="all">Tất cả</TabsTrigger>
            <TabsTrigger value="on">On</TabsTrigger>
            <TabsTrigger value="off">Off</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {/* Table */}
      <div className="rounded-md border border-hairline bg-canvas" style={{ boxShadow: "var(--shadow-e2)" }}>
        <Table>
          <TableHeader>
            <TableRow>
              {SORTABLE.map(([col, label]) => (
                <TableHead key={col}>
                  <button
                    onClick={() => toggleSort(col)}
                    className="inline-flex items-center gap-1 hover:text-ink"
                  >
                    {label}
                    {sortBy === col ? (
                      sortOrder === "asc" ? (
                        <ArrowUp className="size-3" />
                      ) : (
                        <ArrowDown className="size-3" />
                      )
                    ) : null}
                  </button>
                </TableHead>
              ))}
              <TableHead className="text-right">Thao tác</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 8 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: 7 }).map((__, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : servers.length ? (
              servers.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="font-mono text-ink">{s.server_id}</TableCell>
                  <TableCell className="text-ink">{s.server_name}</TableCell>
                  <TableCell>
                    <StatusPill status={s.status} />
                  </TableCell>
                  <TableCell className="font-mono">{s.ipv4}</TableCell>
                  <TableCell>{orDash(s.location)}</TableCell>
                  <TableCell className="text-mute">{s.created_at?.slice(0, 10)}</TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <Button asChild variant="ghost" size="icon" title="Xem">
                        <Link href={`/servers/${s.server_id}`}>
                          <Eye />
                        </Link>
                      </Button>
                      <Can scope="server:update">
                        <Button variant="ghost" size="icon" title="Sửa" onClick={() => setEditing(s)}>
                          <Pencil />
                        </Button>
                      </Can>
                      <Can scope="server:delete">
                        <Button
                          variant="ghost"
                          size="icon"
                          title="Xoá"
                          className="text-error hover:text-error-deep"
                          onClick={() => setDeleting(s)}
                        >
                          <Trash2 />
                        </Button>
                      </Can>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell colSpan={7} className="p-0">
                  <EmptyState
                    title={isError ? "Không tải được dữ liệu" : "Không có server"}
                    description={
                      isError
                        ? "Thử lại sau."
                        : hasSearch
                          ? "Không có server khớp từ khoá tìm kiếm."
                          : "Chưa có server nào."
                    }
                    action={
                      hasSearch && !isError ? (
                        <Button variant="secondary" size="sm" onClick={clearSearch}>
                          <X /> Xoá tìm kiếm & tải lại toàn bộ
                        </Button>
                      ) : undefined
                    }
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
          onChange={(p) => setParam({ page: String(p) })}
        />
      ) : null}

      {/* Dialogs */}
      <ServerFormDialog open={createOpen} onOpenChange={setCreateOpen} />
      <ServerFormDialog
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        server={editing}
      />
      {deleting ? (
        <DeleteServerDialog
          serverId={deleting.server_id}
          serverName={deleting.server_name}
          open={!!deleting}
          onOpenChange={(o) => !o && setDeleting(null)}
        />
      ) : null}
      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
    </div>
  );
}

export default function ServersPage() {
  return (
    <Suspense fallback={<Skeleton className="h-96 w-full" />}>
      <ServersView />
    </Suspense>
  );
}
