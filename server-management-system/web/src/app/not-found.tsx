import Link from "next/link";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <div className="grid min-h-screen place-items-center px-4">
      <div className="flex flex-col items-center gap-4 text-center">
        <h1 className="display-md text-ink">404 — Không tìm thấy</h1>
        <p className="text-sm text-body">Trang bạn tìm không tồn tại.</p>
        <Button asChild variant="secondary">
          <Link href="/">Về trang chủ</Link>
        </Button>
      </div>
    </div>
  );
}
