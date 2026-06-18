"use client";

import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  LabelList,
  Pie,
  PieChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { ServerUptime } from "@/lib/api/types";

const INK = "#171717";
const LINK = "#0070f3";
const MUTE = "#c4c4c4";
const WARN = "#f5a623";
const ERR = "#ee0000";
const HAIRLINE = "#ebebeb";

const numberFmt = new Intl.NumberFormat("vi-VN");

export function OnOffDonut({ on, off }: { on: number; off: number }) {
  const data = [
    { name: "On", value: on, color: LINK },
    { name: "Off", value: off, color: MUTE },
  ];
  const total = on + off;
  const onPct = total ? (on / total) * 100 : 0;
  const offPct = total ? (off / total) * 100 : 0;

  if (total === 0) {
    return <p className="py-16 text-center text-sm text-mute">Không có dữ liệu</p>;
  }

  return (
    <div className="space-y-4">
      <div className="relative h-[260px]">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            <Pie
              data={data}
              dataKey="value"
              nameKey="name"
              innerRadius={68}
              outerRadius={104}
              paddingAngle={3}
              cornerRadius={6}
              strokeWidth={0}
            >
              {data.map((d) => (
                <Cell key={d.name} fill={d.color} />
              ))}
            </Pie>
            <Tooltip
              contentStyle={{ borderRadius: 8, border: `1px solid ${HAIRLINE}`, fontSize: 12 }}
              formatter={(value, name) => [numberFmt.format(Number(value)), String(name)]}
            />
          </PieChart>
        </ResponsiveContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-xs font-medium uppercase tracking-normal text-mute">Tổng</span>
          <span className="text-3xl font-semibold text-ink">{numberFmt.format(total)}</span>
          <span className="text-xs text-body">server</span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 text-sm">
        <div className="rounded-sm border border-hairline bg-canvas-soft p-3">
          <div className="mb-2 flex items-center gap-2 text-body">
            <span className="h-2.5 w-2.5 rounded-full bg-link" />
            On
          </div>
          <div className="flex items-baseline justify-between gap-2">
            <span className="text-xl font-semibold text-ink">{numberFmt.format(on)}</span>
            <span className="font-mono text-xs text-mute">{onPct.toFixed(1)}%</span>
          </div>
        </div>
        <div className="rounded-sm border border-hairline bg-canvas-soft p-3">
          <div className="mb-2 flex items-center gap-2 text-body">
            <span className="h-2.5 w-2.5 rounded-full bg-hairline-strong" />
            Off
          </div>
          <div className="flex items-baseline justify-between gap-2">
            <span className="text-xl font-semibold text-ink">{numberFmt.format(off)}</span>
            <span className="font-mono text-xs text-mute">{offPct.toFixed(1)}%</span>
          </div>
        </div>
      </div>
    </div>
  );
}

export function LowUptimeBar({ data }: { data: ServerUptime[] }) {
  if (!data.length) {
    return <p className="py-16 text-center text-sm text-mute">Không có server uptime thấp</p>;
  }
  const chartData = data.map((s) => ({
    name: s.server_name || s.server_id,
    serverId: s.server_id,
    uptime: Number(s.uptime_pct.toFixed(2)),
  }));
  const barColor = (v: number) => (v < 90 ? ERR : v < 95 ? WARN : LINK);

  return (
    <ResponsiveContainer width="100%" height={Math.max(260, chartData.length * 42)}>
      <BarChart data={chartData} layout="vertical" margin={{ top: 8, right: 48, bottom: 8, left: 12 }}>
        <CartesianGrid stroke={HAIRLINE} horizontal={false} strokeDasharray="3 3" />
        <XAxis
          type="number"
          domain={[0, 100]}
          tick={{ fontSize: 11, fill: "#888" }}
          tickLine={false}
          axisLine={false}
          unit="%"
        />
        <YAxis
          type="category"
          dataKey="name"
          width={132}
          tick={{ fontSize: 12, fill: "#4d4d4d" }}
          tickLine={false}
          axisLine={false}
        />
        <ReferenceLine x={95} stroke={WARN} strokeDasharray="4 4" />
        <Tooltip
          contentStyle={{ borderRadius: 8, border: `1px solid ${HAIRLINE}`, fontSize: 12 }}
          formatter={(value) => [`${value}%`, "Uptime"]}
          labelFormatter={(_, payload) => payload?.[0]?.payload?.serverId ?? ""}
        />
        <Bar dataKey="uptime" radius={[0, 5, 5, 0]} barSize={22} background={{ fill: "#f5f5f5", radius: 5 }}>
          {chartData.map((d) => (
            <Cell key={d.serverId} fill={barColor(d.uptime)} />
          ))}
          <LabelList dataKey="uptime" position="right" formatter={(value) => `${value ?? ""}%`} fill={INK} fontSize={12} />
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}
