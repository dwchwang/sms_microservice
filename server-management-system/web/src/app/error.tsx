"use client";

import { useEffect } from "react";
import { Button } from "@/components/ui/button";

export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error(error);
  }, [error]);

  return (
    <div className="grid min-h-screen place-items-center px-4">
      <div className="flex flex-col items-center gap-4 text-center">
        <h1 className="display-md text-ink">Đã xảy ra lỗi</h1>
        <p className="max-w-md text-sm text-body">
          Có sự cố không mong muốn. Vui lòng thử lại.
        </p>
        <Button onClick={reset}>Thử lại</Button>
      </div>
    </div>
  );
}
