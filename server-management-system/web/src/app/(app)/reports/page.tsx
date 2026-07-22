"use client";

import { useState } from "react";
import { format, subDays } from "date-fns";
import { Mail, RefreshCw } from "lucide-react";
import { useServerUptime } from "@/lib/api/hooks";
import { Can } from "@/components/common/can";
import { PageHeader } from "@/components/common/page-header";
import { KpiCard } from "@/components/common/kpi-card";
import { EmptyState } from "@/components/common/empty-state";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { StatusDonut, LowUptimeBar } from "@/components/reports/charts";
import { SendReportDialog } from "@/components/reports/send-report-dialog";

const REFRESH_MS = 60_000;

// The email report covers whole finished days; the dashboard below is live.
const YESTERDAY = format(subDays(new Date(), 1), "yyyy-MM-dd");
const WEEK_AGO = format(subDays(new Date(), 7), "yyyy-MM-dd");

const numberFmt = new Intl.NumberFormat("vi-VN");

function pct(v: number) {
  return `${v.toFixed(2)}%`;
}

export default function ReportsPage() {
  const [sendOpen, setSendOpen] = useState(false);
  const { data, isLoading, isFetching, isError, error, refetch, dataUpdatedAt } =
    useServerUptime({ refetchInterval: REFRESH_MS });

  const top = data?.top_10_lowest_uptime ?? [];
  const lastUpdated = dataUpdatedAt ? format(new Date(dataUpdatedAt), "HH:mm:ss") : "—";

  return (
    <div>
      <PageHeader
        title="Dashboard uptime"
        description="Uptime trong ngày hôm nay (từ 00:00 giờ Việt Nam), cập nhật mỗi phút."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs text-mute">Cập nhật {lastUpdated}</span>
            <Button
              variant="secondary"
              onClick={() => refetch()}
              disabled={isFetching}
              aria-label="Làm mới dữ liệu"
            >
              <RefreshCw className={isFetching ? "animate-spin" : undefined} />
              Làm mới
            </Button>
            <Can scope="report:send">
              <Button onClick={() => setSendOpen(true)}>
                <Mail /> Gửi báo cáo qua email
              </Button>
            </Can>
          </div>
        }
      />

      {isLoading ? (
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-4">
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
          </div>
          <Skeleton className="h-72" />
        </div>
      ) : isError || !data ? (
        <EmptyState
          title="Không tải được dữ liệu"
          description={error?.message ?? "Thử lại sau."}
        />
      ) : (
        <div className="space-y-6">
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <KpiCard label="Tổng server" value={numberFmt.format(data.total_servers)} />
            <KpiCard
              label="Đang On"
              value={numberFmt.format(data.servers_on)}
              hint="Trạng thái hiện tại"
              accent="success"
            />
            <KpiCard
              label="Đang Off"
              value={numberFmt.format(data.servers_off)}
              hint="Trạng thái hiện tại"
              accent="mute"
            />
            <KpiCard
              label="Uptime trung bình hôm nay"
              value={data.avg_uptime_pct === null ? "—" : pct(data.avg_uptime_pct)}
              hint={
                data.servers_no_data > 0
                  ? `${numberFmt.format(data.servers_no_data)} server chưa được check hôm nay`
                  : "Trên toàn bộ server đã đo hôm nay"
              }
              accent={
                data.avg_uptime_pct !== null && data.avg_uptime_pct < 95 ? "warning" : "ink"
              }
            />
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Trạng thái hiện tại</CardTitle>
            </CardHeader>
            <CardContent>
              <StatusDonut
                on={data.servers_on}
                off={data.servers_off}
                unknown={data.servers_unknown}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Top 10 server uptime thấp nhất hôm nay</CardTitle>
            </CardHeader>
            <CardContent>
              <LowUptimeBar data={top} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Chi tiết hôm nay</CardTitle>
            </CardHeader>
            <CardContent className="pt-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Server ID</TableHead>
                    <TableHead>Tên</TableHead>
                    <TableHead className="text-right">On / Tổng check hôm nay</TableHead>
                    <TableHead className="text-right">Uptime</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {top.length ? (
                    top.map((r) => (
                      <TableRow key={r.server_id}>
                        <TableCell className="font-mono text-ink">{r.server_id}</TableCell>
                        <TableCell>{r.server_name}</TableCell>
                        <TableCell className="text-right font-mono text-mute">
                          {numberFmt.format(r.on_checks)} / {numberFmt.format(r.total_checks)}
                        </TableCell>
                        <TableCell
                          className={`text-right font-mono font-medium ${
                            r.uptime_pct < 90
                              ? "text-error"
                              : r.uptime_pct < 95
                                ? "text-warning-deep"
                                : "text-ink"
                          }`}
                        >
                          {pct(r.uptime_pct)}
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={4} className="py-8 text-center text-mute">
                        Chưa có server nào được check hôm nay
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </div>
      )}

      <SendReportDialog
        open={sendOpen}
        onOpenChange={setSendOpen}
        defaultStart={WEEK_AGO}
        defaultEnd={YESTERDAY}
      />
    </div>
  );
}
