"use client";

import { Suspense, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import {
  Plus,
  Upload,
  Search,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  Pencil,
  Trash2,
  Eye,
  X,
  RefreshCw,
} from "lucide-react";
import { useServers } from "@/lib/api/hooks";
import type { ServerListParams } from "@/lib/api/endpoints";
import type { ServerResponse, ServerStatus } from "@/lib/api/types";
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

// Status is deliberately not sortable: the tabs above already filter by it.
const COLUMNS = [
  { key: "server_id", label: "ID", sortable: true },
  { key: "server_name", label: "Tên server", sortable: true },
  { key: "status", label: "Trạng thái", sortable: false },
  { key: "ipv4", label: "IPv4", sortable: true },
  { key: "tcp_port", label: "Port", sortable: true },
  { key: "location", label: "Vị trí", sortable: true },
  { key: "created_at", label: "Ngày tạo", sortable: true },
  { key: "updated_at", label: "Cập nhật gần nhất", sortable: true },
] as const;

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

function numberParam(value: string | null, fallback: number) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.trunc(parsed) : fallback;
}

function pageSizeParam(value: string | null) {
  const parsed = numberParam(value, 20);
  return PAGE_SIZE_OPTIONS.includes(parsed) ? parsed : 20;
}

function formatDateTime(value?: string) {
  if (!value) return "-";

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;

  return new Intl.DateTimeFormat("vi-VN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function ServersView() {
  const router = useRouter();
  const sp = useSearchParams();

  const [idSearch, setIdSearch] = useState(sp.get("server_id") ?? "");
  const [nameSearch, setNameSearch] = useState(sp.get("server_name") ?? "");
  const [ipSearch, setIpSearch] = useState(sp.get("ipv4") ?? "");
  const [portSearch, setPortSearch] = useState(sp.get("tcp_port") ?? "");
  const [importOpen, setImportOpen] = useState(sp.get("import") === "1");
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<ServerResponse | null>(null);
  const [deleting, setDeleting] = useState<ServerResponse | null>(null);

  const status = (sp.get("status") as ServerStatus | null) ?? undefined;
  const sortBy = sp.get("sort_by") ?? "created_at";
  const sortOrder = (sp.get("sort_order") as "asc" | "desc") ?? "desc";
  const page = numberParam(sp.get("page"), 1);
  const pageSize = pageSizeParam(sp.get("page_size"));
  const serverId = sp.get("server_id") ?? undefined;
  const serverName = sp.get("server_name") ?? undefined;
  const ipv4 = sp.get("ipv4") ?? undefined;
  const tcpPortParam = sp.get("tcp_port");
  const tcpPort =
    tcpPortParam && Number.isFinite(Number(tcpPortParam)) ? Number(tcpPortParam) : undefined;

  const params: ServerListParams = useMemo(
    () => ({
      page,
      page_size: pageSize,
      status,
      server_id: serverId,
      server_name: serverName,
      ipv4,
      tcp_port: tcpPort,
      sort_by: sortBy,
      sort_order: sortOrder,
    }),
    [page, pageSize, status, serverId, serverName, ipv4, tcpPort, sortBy, sortOrder],
  );

  const { data, isLoading, isError, isFetching, refetch } = useServers(params);

  function setParam(updates: Record<string, string | undefined>) {
    const next = new URLSearchParams(sp.toString());
    next.delete("import");
    for (const [k, v] of Object.entries(updates)) {
      if (v === undefined || v === "") next.delete(k);
      else next.set(k, v);
    }
    const query = next.toString();
    router.push(query ? `/servers?${query}` : "/servers");
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
    setParam({
      server_id: idSearch.trim() || undefined,
      server_name: nameSearch.trim() || undefined,
      ipv4: ipSearch.trim() || undefined,
      tcp_port: portSearch.trim() || undefined,
      page: "1",
    });
  }

  // Xoá các ô lọc và tải lại toàn bộ server.
  function clearSearch() {
    setIdSearch("");
    setNameSearch("");
    setIpSearch("");
    setPortSearch("");
    setParam({
      server_id: undefined,
      server_name: undefined,
      ipv4: undefined,
      tcp_port: undefined,
      page: "1",
    });
  }

  const servers = data?.servers ?? [];
  const hasSearch =
    idSearch.length > 0 ||
    nameSearch.length > 0 ||
    ipSearch.length > 0 ||
    portSearch.length > 0 ||
    !!serverId ||
    !!serverName ||
    !!ipv4 ||
    tcpPort !== undefined;

  return (
    <div>
      <PageHeader
        title="Servers"
        description="Quản lý danh sách server, trạng thái On/Off cập nhật mỗi 60s."
        actions={
          <>
            <Button variant="secondary" onClick={() => refetch()} disabled={isFetching}>
              <RefreshCw className={isFetching ? "animate-spin" : undefined} /> Refresh
            </Button>
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
        <form onSubmit={submitSearch} className="grid flex-1 grid-cols-1 gap-2 md:grid-cols-[1fr_1fr_1fr_0.7fr_auto] xl:max-w-5xl">
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-mute" />
            <Input
              value={idSearch}
              onChange={(e) => setIdSearch(e.target.value)}
              placeholder="Tìm ID..."
              className="pl-9 font-mono"
            />
          </div>
          <div className="relative flex-1">
            <Input
              value={nameSearch}
              onChange={(e) => setNameSearch(e.target.value)}
              placeholder="Tìm tên server..."
            />
          </div>
          <div className="relative flex-1">
            <Input
              value={ipSearch}
              onChange={(e) => setIpSearch(e.target.value)}
              placeholder="Lọc IPv4..."
              className="font-mono"
            />
          </div>
          <div className="relative flex-1">
            <Input
              value={portSearch}
              onChange={(e) => setPortSearch(e.target.value.replace(/\D/g, "").slice(0, 5))}
              inputMode="numeric"
              placeholder="Lọc port..."
              className={hasSearch ? "pr-9 font-mono" : "font-mono"}
            />
            {hasSearch ? (
              <button
                type="button"
                onClick={clearSearch}
                title="Xoá bộ lọc & tải lại toàn bộ"
                aria-label="Xoá bộ lọc"
                className="absolute right-2 top-1/2 grid size-6 -translate-y-1/2 place-items-center rounded-full text-mute hover:bg-canvas-soft-2 hover:text-ink"
              >
                <X className="size-4" />
              </button>
            ) : null}
          </div>
          <Button type="submit" className="w-full md:w-auto">
            <Search /> Search
          </Button>
        </form>
        <Tabs
          value={status ?? "all"}
          onValueChange={(v) => setParam({ status: v === "all" ? undefined : v, page: "1" })}
        >
          <TabsList>
            <TabsTrigger value="all">Tất cả</TabsTrigger>
            <TabsTrigger value="ON">On</TabsTrigger>
            <TabsTrigger value="OFF">Off</TabsTrigger>
            <TabsTrigger value="UNKNOWN">Chưa rõ</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {/* Table */}
      <div className="rounded-md border border-hairline bg-canvas" style={{ boxShadow: "var(--shadow-e2)" }}>
        <Table>
          <TableHeader>
            <TableRow>
              {COLUMNS.map(({ key, label, sortable }) =>
                sortable ? (
                  // aria-sort stays on the th, whose implicit role is
                  // columnheader; a real button inside supplies the activation,
                  // so Enter, Space and focus all come from the platform.
                  <TableHead
                    key={key}
                    aria-sort={
                      sortBy === key
                        ? sortOrder === "asc"
                          ? "ascending"
                          : "descending"
                        : "none"
                    }
                    className="p-0"
                  >
                    <button
                      type="button"
                      title={`Sắp xếp theo ${label}`}
                      onClick={() => toggleSort(key)}
                      className="group flex h-9 w-full cursor-pointer select-none items-center justify-between gap-2 whitespace-nowrap px-3 text-left hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-link"
                    >
                      <span>{label}</span>
                      <span className="grid size-4 place-items-center">
                        {sortBy === key ? (
                          sortOrder === "asc" ? (
                            <ArrowUp className="size-3.5 text-ink" />
                          ) : (
                            <ArrowDown className="size-3.5 text-ink" />
                          )
                        ) : (
                          <ArrowUpDown className="size-3.5 opacity-35 group-hover:opacity-80" />
                        )}
                      </span>
                    </button>
                  </TableHead>
                ) : (
                  <TableHead key={key}>
                    <span className="flex h-9 items-center whitespace-nowrap">{label}</span>
                  </TableHead>
                ),
              )}
              <TableHead className="text-right">Thao tác</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: 8 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: 9 }).map((__, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : servers.length ? (
              servers.map((s) => (
                <TableRow key={s.server_id}>
                  <TableCell className="font-mono text-ink">{s.server_id}</TableCell>
                  <TableCell className="text-ink">{s.server_name}</TableCell>
                  <TableCell>
                    <StatusPill status={s.status} />
                  </TableCell>
                  <TableCell className="font-mono">{s.ipv4}</TableCell>
                  <TableCell className="font-mono text-mute">{s.tcp_port}</TableCell>
                  <TableCell>{orDash(s.location)}</TableCell>
                  <TableCell className="text-mute">{formatDateTime(s.created_at)}</TableCell>
                  <TableCell className="text-mute">{formatDateTime(s.updated_at)}</TableCell>
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
                <TableCell colSpan={9} className="p-0">
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
                          <X /> Xoá bộ lọc & tải lại toàn bộ
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
          pageSize={data.page_size}
          pageSizeOptions={PAGE_SIZE_OPTIONS}
          itemLabel="server"
          onChange={(p) => setParam({ page: String(p) })}
          onPageSizeChange={(size) => setParam({ page_size: String(size), page: "1" })}
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
