import { Card } from "@/components/ui/card";

export function AuthShell({
  title,
  subtitle,
  children,
  footer,
}: {
  title: string;
  subtitle: string;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <div className="relative flex min-h-screen items-center justify-center px-4 py-12">
      {/* Brand mesh gradient — hero scale only */}
      <div className="mesh-gradient pointer-events-none absolute inset-x-0 top-0 h-64 opacity-60 blur-2xl" />
      <div className="relative w-full max-w-md">
        <div className="mb-6 text-center">
          <p className="font-mono text-xs uppercase tracking-widest text-mute">VCS · SMS</p>
          <h1 className="mt-2 display-lg text-ink">{title}</h1>
          <p className="mt-1 text-sm text-body">{subtitle}</p>
        </div>
        <Card className="p-6" style={{ boxShadow: "var(--shadow-e4)" }}>
          {children}
        </Card>
        {footer ? <p className="mt-4 text-center text-sm text-body">{footer}</p> : null}
      </div>
    </div>
  );
}
