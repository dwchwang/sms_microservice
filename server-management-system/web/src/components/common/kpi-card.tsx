import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function KpiCard({
  label,
  value,
  hint,
  accent,
}: {
  label: string;
  value: React.ReactNode;
  hint?: string;
  accent?: "ink" | "success" | "mute" | "warning";
}) {
  const valueColor =
    accent === "success"
      ? "text-link"
      : accent === "mute"
        ? "text-mute"
        : accent === "warning"
          ? "text-warning-deep"
          : "text-ink";
  return (
    <Card className="p-5">
      <p className="font-mono text-xs uppercase tracking-wide text-mute">{label}</p>
      <p className={cn("mt-2 display-lg", valueColor)}>{value}</p>
      {hint ? <p className="mt-1 text-sm text-body">{hint}</p> : null}
    </Card>
  );
}
