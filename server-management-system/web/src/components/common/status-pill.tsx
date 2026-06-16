import { Badge } from "@/components/ui/badge";
import type { Role, ServerStatus } from "@/lib/api/types";

export function StatusPill({ status }: { status: ServerStatus }) {
  return (
    <Badge variant={status === "on" ? "success" : "neutral"}>
      <span
        className={`inline-block size-1.5 rounded-full ${status === "on" ? "bg-link" : "bg-mute"}`}
      />
      {status === "on" ? "On" : "Off"}
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
