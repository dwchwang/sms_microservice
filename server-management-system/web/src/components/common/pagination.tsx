"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";

export function Pagination({
  page,
  totalPages,
  total,
  onChange,
}: {
  page: number;
  totalPages: number;
  total: number;
  onChange: (page: number) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 py-3">
      <p className="text-sm text-body">
        Trang <span className="font-medium text-ink">{page}</span> / {Math.max(totalPages, 1)} ·{" "}
        <span className="font-mono">{total.toLocaleString("vi-VN")}</span> bản ghi
      </p>
      <div className="flex gap-2">
        <Button
          variant="secondary"
          size="sm"
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
        >
          <ChevronLeft /> Trước
        </Button>
        <Button
          variant="secondary"
          size="sm"
          disabled={page >= totalPages}
          onClick={() => onChange(page + 1)}
        >
          Sau <ChevronRight />
        </Button>
      </div>
    </div>
  );
}
