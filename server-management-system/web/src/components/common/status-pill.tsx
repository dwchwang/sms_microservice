import { Badge } from "@/components/ui/badge";
import type { Role, ServerStatus } from "@/lib/api/types";

// UNKNOWN is its own state, not a kind of Off: nobody has checked it yet.
const statusStyle: Record<
  ServerStatus,
  { variant: "success" | "neutral" | "warning"; dot: string; label: string }
> = {
  ON: { variant: "success", dot: "bg-link", label: "On" },
  OFF: { variant: "neutral", dot: "bg-mute", label: "Off" },
  UNKNOWN: { variant: "warning", dot: "bg-mute", label: "Chưa rõ" },
};

export function StatusPill({ status }: { status: ServerStatus }) {
  const s = statusStyle[status] ?? statusStyle.UNKNOWN;
  return (
    <Badge variant={s.variant}>
      <span className={`inline-block size-1.5 rounded-full ${s.dot}`} />
      {s.label}
    </Badge>
  );
}

const roleVariant: Record<Role, "ink" | "warning" | "neutral"> = {
  admin: "ink",
  operator: "warning",
  viewer: "neutral",
};

export function RoleBadge({ role }: { role: Role }) {
  return <Badge variant={roleVariant[role] ?? "neutral"}>{role}</Badge>;
}
