"use client";

import { useState } from "react";
import { format, subDays } from "date-fns";
import { Mail, BarChart3 } from "lucide-react";
import { useReportSummary } from "@/lib/api/hooks";
import { Can } from "@/components/common/can";
import { PageHeader } from "@/components/common/page-header";
import { KpiCard } from "@/components/common/kpi-card";
import { EmptyState } from "@/components/common/empty-state";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { OnOffDonut, LowUptimeBar } from "@/components/reports/charts";
import { SendReportDialog } from "@/components/reports/send-report-dialog";

const TODAY = format(new Date(), "yyyy-MM-dd");
const WEEK_AGO = format(subDays(new Date(), 6), "yyyy-MM-dd");

export default function ReportsPage() {
  const [start, setStart] = useState(WEEK_AGO);
  const [end, setEnd] = useState(TODAY);
  const [applied, setApplied] = useState({ start: WEEK_AGO, end: TODAY });
  const [sendOpen, setSendOpen] = useState(false);

  const { data, isLoading, isError, error } = useReportSummary(applied.start, applied.end, true);
  const lowUptime = data?.low_uptime_servers ?? [];

  return (
    <div>
      <PageHeader
        title="Báo cáo uptime"
        description="Thống kê trạng thái & uptime server theo khoảng thời gian."
        actions={
          <Can scope="report:send">
            <Button onClick={() => setSendOpen(true)}>
              <Mail /> Gửi qua email
            </Button>
          </Can>
        }
      />

      {/* Date range */}
      <Card className="mb-6 p-4">
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Từ ngày">
            <Input type="date" value={start} max={end} onChange={(e) => setStart(e.target.value)} />
          </Field>
          <Field label="Đến ngày">
            <Input type="date" value={end} min={start} max={TODAY} onChange={(e) => setEnd(e.target.value)} />
          </Field>
          <Button variant="secondary" onClick={() => setApplied({ start, end })}>
            <BarChart3 /> Xem báo cáo
          </Button>
        </div>
      </Card>

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
          title="Không tải được báo cáo"
          description={error?.message ?? "Kiểm tra khoảng ngày hoặc thử lại sau."}
        />
      ) : (
        <div className="space-y-6">
          {/* KPIs */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <KpiCard label="Tổng server" value={data.total_servers?.toLocaleString("vi-VN") ?? "—"} />
            <KpiCard label="On" value={data.servers_on?.toLocaleString("vi-VN") ?? "—"} accent="success" />
            <KpiCard label="Off" value={data.servers_off?.toLocaleString("vi-VN") ?? "—"} accent="mute" />
            <KpiCard
              label="Uptime trung bình"
              value={typeof data.avg_uptime_pct === "number" ? `${data.avg_uptime_pct.toFixed(2)}%` : "—"}
              hint={`${(data.total_checks ?? 0).toLocaleString("vi-VN")} lượt check`}
              accent={data.avg_uptime_pct < 95 ? "warning" : "ink"}
            />
          </div>

          {/* Charts */}
          <div className="grid gap-4 lg:grid-cols-3">
            <Card>
              <CardHeader>
                <CardTitle>Tỉ lệ On / Off</CardTitle>
              </CardHeader>
              <CardContent>
                <OnOffDonut on={data.servers_on ?? 0} off={data.servers_off ?? 0} />
              </CardContent>
            </Card>
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Uptime — server thấp nhất</CardTitle>
              </CardHeader>
              <CardContent>
                <LowUptimeBar data={lowUptime} />
              </CardContent>
            </Card>
          </div>

          {/* Low uptime table */}
          <Card>
            <CardHeader>
              <CardTitle>Chi tiết server uptime thấp</CardTitle>
            </CardHeader>
            <CardContent className="pt-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Server ID</TableHead>
                    <TableHead>Tên</TableHead>
                    <TableHead className="text-right">On / Tổng</TableHead>
                    <TableHead className="text-right">Uptime</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {lowUptime.length ? (
                    lowUptime.map((r) => (
                      <TableRow key={r.server_id}>
                        <TableCell className="font-mono text-ink">{r.server_id}</TableCell>
                        <TableCell>{r.server_name}</TableCell>
                        <TableCell className="text-right font-mono text-mute">
                          {r.on_checks?.toLocaleString("vi-VN")} / {r.total_checks?.toLocaleString("vi-VN")}
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
                          {r.uptime_pct.toFixed(2)}%
                        </TableCell>
                      </TableRow>
                    ))
                  ) : (
                    <TableRow>
                      <TableCell colSpan={4} className="py-8 text-center text-mute">
                        Không có server uptime thấp 🎉
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
        defaultStart={applied.start}
        defaultEnd={applied.end}
      />
    </div>
  );
}
