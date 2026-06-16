"use client";

import Link from "next/link";
import { Server, BarChart3, Upload, RefreshCw } from "lucide-react";
import { useServers, useMonitorStatus } from "@/lib/api/hooks";
import { useAuth } from "@/providers/auth-provider";
import { Can } from "@/components/common/can";
import { KpiCard } from "@/components/common/kpi-card";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn, orDash } from "@/lib/utils";

const REFRESH_MS = 60_000; // auto-refresh mỗi phút

function useServerCounts() {
  const opt = { refetchInterval: REFRESH_MS };
  const all = useServers({ page: 1, page_size: 1 }, opt);
  const on = useServers({ page: 1, page_size: 1, status: "on" }, opt);
  const off = useServers({ page: 1, page_size: 1, status: "off" }, opt);
  return {
    total: all.data?.total,
    on: on.data?.total,
    off: off.data?.total,
    loading: all.isLoading || on.isLoading || off.isLoading,
    fetching: all.isFetching || on.isFetching || off.isFetching,
    updatedAt: all.dataUpdatedAt,
    refetch: () => {
      void all.refetch();
      void on.refetch();
      void off.refetch();
    },
  };
}

export default function DashboardPage() {
  const { user } = useAuth();
  const counts = useServerCounts();
  const monitor = useMonitorStatus();

  const refreshing = counts.fetching || monitor.isFetching;

  function refreshAll() {
    counts.refetch();
    void monitor.refetch();
  }

  const updatedLabel = counts.updatedAt
    ? new Date(counts.updatedAt).toLocaleTimeString("vi-VN")
    : "—";

  return (
    <div className="space-y-8">
      {/* Hero */}
      <div className="relative overflow-hidden rounded-lg border border-hairline bg-canvas p-8">
        <div className="mesh-gradient pointer-events-none absolute -right-20 -top-20 size-72 rounded-full opacity-30 blur-3xl" />
        <div className="relative flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <p className="font-mono text-xs uppercase tracking-widest text-mute">Tổng quan</p>
            <h1 className="mt-2 display-lg text-ink">Xin chào, {user?.full_name}.</h1>
            <p className="mt-1 text-sm text-body">
              Theo dõi trạng thái 10.000 server theo thời gian thực, tự động cập nhật mỗi phút.
            </p>
          </div>
          <div className="flex flex-col items-start gap-1 sm:items-end">
            <Button variant="secondary" size="sm" onClick={refreshAll} disabled={refreshing}>
              <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
              Làm mới
            </Button>
            <span className="font-mono text-xs text-mute">Cập nhật lúc {updatedLabel}</span>
          </div>
        </div>
      </div>

      {/* KPIs */}
      <Can scope="server:read">
        <div className="grid gap-4 sm:grid-cols-3">
          {counts.loading ? (
            <>
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
            </>
          ) : (
            <>
              <KpiCard label="Tổng server" value={orDash(counts.total?.toLocaleString("vi-VN"))} />
              <KpiCard label="Đang On" value={orDash(counts.on?.toLocaleString("vi-VN"))} accent="success" />
              <KpiCard label="Đang Off" value={orDash(counts.off?.toLocaleString("vi-VN"))} accent="mute" />
            </>
          )}
        </div>
      </Can>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Quick actions */}
        <Card>
          <CardHeader>
            <CardTitle>Thao tác nhanh</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2">
            <Can scope="server:read">
              <Button asChild variant="secondary">
                <Link href="/servers">
                  <Server /> Quản lý servers
                </Link>
              </Button>
            </Can>
            <Can scope="report:view">
              <Button asChild variant="secondary">
                <Link href="/reports">
                  <BarChart3 /> Xem báo cáo
                </Link>
              </Button>
            </Can>
            <Can scope="server:import">
              <Button asChild variant="secondary">
                <Link href="/servers?import=1">
                  <Upload /> Import Excel
                </Link>
              </Button>
            </Can>
          </CardContent>
        </Card>

        {/* Monitor widget */}
        <Card>
          <CardHeader className="flex-row items-center justify-between">
            <CardTitle>Dịch vụ giám sát</CardTitle>
            <span
              className={cn(
                "flex items-center gap-1.5 text-xs",
                monitor.isError ? "text-mute" : "text-link",
              )}
            >
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  monitor.isError ? "bg-mute" : "bg-link",
                )}
              />
              {monitor.isError ? "Offline" : "Live"}
            </span>
          </CardHeader>
          <CardContent>
            {monitor.isLoading ? (
              <Skeleton className="h-24" />
            ) : monitor.isError ? (
              <div className="space-y-3">
                <p className="text-sm text-body">Không truy cập được monitor service.</p>
                <Button variant="secondary" size="sm" onClick={() => monitor.refetch()}>
                  <RefreshCw className="size-4" /> Thử lại
                </Button>
              </div>
            ) : (
              <dl className="grid grid-cols-2 gap-3 text-sm">
                <div>
                  <dt className="text-mute">Trạng thái</dt>
                  <dd className="font-medium text-ink">{orDash(monitor.data?.status)}</dd>
                </div>
                <div>
                  <dt className="text-mute">Chu kỳ check</dt>
                  <dd className="font-mono text-ink">{orDash(monitor.data?.check_interval)}</dd>
                </div>
                <div>
                  <dt className="text-mute">Workers</dt>
                  <dd className="font-mono text-ink">{orDash(monitor.data?.worker_count)}</dd>
                </div>
                <div>
                  <dt className="text-mute">Redis</dt>
                  <dd className="font-medium text-ink">
                    {monitor.data?.redis_available ? "Sẵn sàng" : "Không"}
                  </dd>
                </div>
                <div className="col-span-2">
                  <dt className="text-mute">Elasticsearch index</dt>
                  <dd className="font-mono text-ink">{orDash(monitor.data?.index)}</dd>
                </div>
              </dl>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
