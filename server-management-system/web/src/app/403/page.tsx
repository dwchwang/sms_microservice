import Link from "next/link";
import { ShieldX } from "lucide-react";
import { Button } from "@/components/ui/button";

export default function ForbiddenPage() {
  return (
    <div className="grid min-h-screen place-items-center px-4">
      <div className="flex flex-col items-center gap-4 text-center">
        <ShieldX className="size-10 text-error" />
        <div>
          <h1 className="display-md text-ink">403 — Không có quyền</h1>
          <p className="mt-1 text-sm text-body">
            Tài khoản của bạn không đủ quyền để truy cập trang này.
          </p>
        </div>
        <Button asChild variant="secondary">
          <Link href="/">Về trang chủ</Link>
        </Button>
      </div>
    </div>
  );
}
