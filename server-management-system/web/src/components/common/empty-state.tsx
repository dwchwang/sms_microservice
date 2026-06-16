import { cn } from "@/lib/utils";

export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-lg bg-canvas-soft px-6 py-16 text-center",
        className,
      )}
    >
      {icon ? <div className="text-mute">{icon}</div> : null}
      <div className="space-y-1">
        <p className="display-sm text-ink">{title}</p>
        {description ? <p className="text-sm text-body">{description}</p> : null}
      </div>
      {action}
    </div>
  );
}
