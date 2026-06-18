"use client";

import { FormEvent, useMemo, useState } from "react";
import { ChevronsLeft, ChevronLeft, ChevronRight, ChevronsRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export function Pagination({
  page,
  totalPages,
  total,
  pageSize,
  pageSizeOptions = [10, 20, 50, 100],
  onChange,
  onPageSizeChange,
  itemLabel = "bản ghi",
}: {
  page: number;
  totalPages: number;
  total: number;
  pageSize?: number;
  pageSizeOptions?: number[];
  onChange: (page: number) => void;
  onPageSizeChange?: (pageSize: number) => void;
  itemLabel?: string;
}) {
  const safeTotalPages = Math.max(totalPages, 1);
  const safePage = Math.min(Math.max(page, 1), safeTotalPages);
  const [pageDraft, setPageDraft] = useState({ page: safePage, value: String(safePage) });
  const pageInput = pageDraft.page === safePage ? pageDraft.value : String(safePage);

  const pages = useMemo<(number | "ellipsis")[]>(() => {
    if (safeTotalPages <= 7) {
      return Array.from({ length: safeTotalPages }, (_, i) => i + 1);
    }

    const values = new Set([1, safeTotalPages, safePage - 1, safePage, safePage + 1]);
    const sorted = [...values]
      .filter((value) => value >= 1 && value <= safeTotalPages)
      .sort((a, b) => a - b);

    return sorted.flatMap((value, index) => {
      const previous = sorted[index - 1];
      return previous && value - previous > 1 ? (["ellipsis", value] as const) : [value];
    });
  }, [safePage, safeTotalPages]);

  const from = total === 0 ? 0 : (safePage - 1) * (pageSize ?? 0) + 1;
  const to = pageSize ? Math.min(safePage * pageSize, total) : total;

  function goTo(nextPage: number) {
    onChange(Math.min(Math.max(nextPage, 1), safeTotalPages));
  }

  function submitPage(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const nextPage = Number(pageInput);
    if (Number.isFinite(nextPage)) goTo(Math.trunc(nextPage));
  }

  return (
    <div className="flex flex-col gap-3 py-3 lg:flex-row lg:items-center lg:justify-between">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-body">
        <p>
          Trang <span className="font-medium text-ink">{safePage}</span> /{" "}
          <span className="font-medium text-ink">{safeTotalPages.toLocaleString("vi-VN")}</span>
        </p>
        <p>
          {pageSize ? (
            <>
              Hiển thị <span className="font-mono text-ink">{from.toLocaleString("vi-VN")}</span>
              {" - "}
              <span className="font-mono text-ink">{to.toLocaleString("vi-VN")}</span> /{" "}
            </>
          ) : null}
          <span className="font-mono text-ink">{total.toLocaleString("vi-VN")}</span> {itemLabel}
        </p>
        {pageSize ? (
          <p>
            <span className="font-mono text-ink">{pageSize.toLocaleString("vi-VN")}</span>{" "}
            {itemLabel}/trang
          </p>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {pageSize && onPageSizeChange ? (
          <Select value={String(pageSize)} onValueChange={(v) => onPageSizeChange(Number(v))}>
            <SelectTrigger className="h-8 w-[116px]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {pageSizeOptions.map((size) => (
                <SelectItem key={size} value={String(size)}>
                  {size} / trang
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}

        <Button
          variant="secondary"
          size="icon"
          title="Trang đầu"
          disabled={safePage <= 1}
          onClick={() => goTo(1)}
        >
          <ChevronsLeft />
        </Button>
        <Button
          variant="secondary"
          size="icon"
          title="Trang trước"
          disabled={safePage <= 1}
          onClick={() => goTo(safePage - 1)}
        >
          <ChevronLeft />
        </Button>

        <div className="hidden items-center gap-1 md:flex">
          {pages.map((value, index) =>
            value === "ellipsis" ? (
              <span key={`ellipsis-${index}`} className="grid h-8 w-8 place-items-center text-mute">
                ...
              </span>
            ) : (
              <Button
                key={value}
                variant={value === safePage ? "primary" : "secondary"}
                size="icon"
                title={`Trang ${value}`}
                onClick={() => goTo(value)}
              >
                {value}
              </Button>
            ),
          )}
        </div>

        <Button
          variant="secondary"
          size="icon"
          title="Trang sau"
          disabled={safePage >= safeTotalPages}
          onClick={() => goTo(safePage + 1)}
        >
          <ChevronRight />
        </Button>
        <Button
          variant="secondary"
          size="icon"
          title="Trang cuối"
          disabled={safePage >= safeTotalPages}
          onClick={() => goTo(safeTotalPages)}
        >
          <ChevronsRight />
        </Button>

        <form onSubmit={submitPage} className="flex items-center gap-2">
          <Input
            type="number"
            min={1}
            max={safeTotalPages}
            value={pageInput}
            onChange={(e) => setPageDraft({ page: safePage, value: e.target.value })}
            aria-label="Nhập trang muốn đến"
            className="h-8 w-20"
          />
          <Button type="submit" variant="secondary" size="sm">
            Đi
          </Button>
        </form>
      </div>
    </div>
  );
}
