"use client";

import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
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

export function OnOffDonut({ on, off }: { on: number; off: number }) {
  const data = [
    { name: "On", value: on, color: LINK },
    { name: "Off", value: off, color: MUTE },
  ];
  const total = on + off;
  if (total === 0) {
    return <p className="py-16 text-center text-sm text-mute">Không có dữ liệu</p>;
  }
  return (
    <ResponsiveContainer width="100%" height={260}>
      <PieChart>
        <Pie data={data} dataKey="value" nameKey="name" innerRadius={60} outerRadius={100} paddingAngle={2}>
          {data.map((d) => (
            <Cell key={d.name} fill={d.color} stroke={INK} strokeWidth={0} />
          ))}
        </Pie>
        <Tooltip contentStyle={{ borderRadius: 8, border: "1px solid #ebebeb", fontSize: 12 }} />
      </PieChart>
    </ResponsiveContainer>
  );
}

export function LowUptimeBar({ data }: { data: ServerUptime[] }) {
  if (!data.length) {
    return <p className="py-16 text-center text-sm text-mute">Không có server uptime thấp</p>;
  }
  const chartData = data.map((s) => ({
    name: s.server_id,
    uptime: Number(s.uptime_pct.toFixed(2)),
  }));
  const barColor = (v: number) => (v < 90 ? ERR : v < 95 ? WARN : LINK);
  return (
    <ResponsiveContainer width="100%" height={Math.max(220, chartData.length * 34)}>
      <BarChart data={chartData} layout="vertical" margin={{ top: 4, right: 16, bottom: 4, left: 8 }}>
        <CartesianGrid stroke="#ebebeb" horizontal={false} />
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
          width={96}
          tick={{ fontSize: 11, fill: "#4d4d4d", fontFamily: "monospace" }}
          tickLine={false}
          axisLine={false}
        />
        <Tooltip
          contentStyle={{ borderRadius: 8, border: "1px solid #ebebeb", fontSize: 12 }}
          formatter={(value) => [`${value}%`, "Uptime"]}
        />
        <Bar dataKey="uptime" radius={[0, 4, 4, 0]}>
          {chartData.map((d) => (
            <Cell key={d.name} fill={barColor(d.uptime)} />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  );
}
