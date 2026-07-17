"use client";

import Link from "next/link";
import { Server, BarChart3, Upload, RefreshCw } from "lucide-react";
import { useServerStats } from "@/lib/api/hooks";
import { useAuth } from "@/providers/auth-provider";
import { Can } from "@/components/common/can";
import { KpiCard } from "@/components/common/kpi-card";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn, orDash } from "@/lib/utils";

const REFRESH_MS = 60_000; // auto-refresh mỗi phút

function useServerCounts() {
  const q = useServerStats({ refetchInterval: REFRESH_MS });
  return {
    total: q.data?.total,
    on: q.data?.on,
    off: q.data?.off,
    unknown: q.data?.unknown,
    loading: q.isLoading,
    fetching: q.isFetching,
    updatedAt: q.dataUpdatedAt,
    refetch: () => void q.refetch(),
  };
}

export default function DashboardPage() {
  const { user } = useAuth();
  const counts = useServerCounts();

  const refreshing = counts.fetching;

  function refreshAll() {
    counts.refetch();
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
      <Can scope="server:stats">
        <div className="grid gap-4 sm:grid-cols-4">
          {counts.loading ? (
            <>
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
            </>
          ) : (
            <>
              <KpiCard label="Tổng server" value={orDash(counts.total?.toLocaleString("vi-VN"))} />
              <KpiCard label="Đang On" value={orDash(counts.on?.toLocaleString("vi-VN"))} accent="success" />
              <KpiCard label="Đang Off" value={orDash(counts.off?.toLocaleString("vi-VN"))} accent="mute" />
              <KpiCard label="Chưa rõ" value={orDash(counts.unknown?.toLocaleString("vi-VN"))} accent="mute" />
            </>
          )}
        </div>
      </Can>

      {/* Quick actions */}
      <Card>
        <CardHeader>
          <CardTitle>Thao tác nhanh</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-2">
          <Can scope="server:list">
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
    </div>
  );
}
