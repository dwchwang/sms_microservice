"use client";

import { useState } from "react";
import { UploadCloud, FileSpreadsheet } from "lucide-react";
import { toast } from "sonner";
import { useImportServers } from "@/lib/api/hooks";
import { errorMessage } from "@/lib/form";
import type { ImportResponse } from "@/lib/api/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/common/spinner";

const MAX_MB = 10;

function IdList({ items, empty }: { items: string[]; empty: string }) {
  return (
    <div className="max-h-64 overflow-y-auto rounded-md border border-hairline">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Server ID</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((id) => (
            <TableRow key={id}>
              <TableCell className="font-mono text-ink">{id}</TableCell>
            </TableRow>
          ))}
          {!items.length ? (
            <TableRow>
              <TableCell className="text-center text-mute">{empty}</TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
    </div>
  );
}

export function ImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [result, setResult] = useState<ImportResponse | null>(null);
  const importMut = useImportServers();

  function pickFile(f: File | undefined) {
    if (!f) return;
    if (!f.name.toLowerCase().endsWith(".xlsx")) {
      toast.error("Chỉ chấp nhận file .xlsx");
      return;
    }
    if (f.size > MAX_MB * 1024 * 1024) {
      toast.error(`File tối đa ${MAX_MB}MB`);
      return;
    }
    setFile(f);
  }

  async function startImport() {
    if (!file) return;
    try {
      const res = await importMut.mutateAsync(file);
      setResult(res);
      toast.success(`Import xong: ${res.succeeded.count}/${res.total_rows} dòng thành công`);
    } catch (err) {
      toast.error(errorMessage(err, "Import thất bại"));
    }
  }

  function close() {
    setFile(null);
    setResult(null);
    onOpenChange(false);
  }

  return (
    <Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import servers từ Excel</DialogTitle>
          <DialogDescription>
            File .xlsx ≤ {MAX_MB}MB. Một dòng lỗi không làm hỏng cả file — các dòng còn
            lại vẫn được nhập.
          </DialogDescription>
        </DialogHeader>

        {!result ? (
          <div className="space-y-4">
            <label className="flex cursor-pointer flex-col items-center justify-center gap-2 rounded-md border border-dashed border-hairline-strong/50 bg-canvas-soft px-6 py-10 text-center hover:bg-canvas-soft-2">
              <UploadCloud className="size-7 text-mute" />
              {file ? (
                <span className="flex items-center gap-2 text-sm text-ink">
                  <FileSpreadsheet className="size-4 text-link" /> {file.name}
                </span>
              ) : (
                <span className="text-sm text-body">Nhấp để chọn file .xlsx</span>
              )}
              <input
                type="file"
                accept=".xlsx"
                className="hidden"
                onChange={(e) => pickFile(e.target.files?.[0])}
              />
            </label>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={close}>
                Huỷ
              </Button>
              <Button onClick={startImport} disabled={!file || importMut.isPending}>
                {importMut.isPending ? <Spinner /> : null}
                Bắt đầu import
              </Button>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="flex flex-wrap gap-2 text-sm">
              <Badge>Tổng: {result.total_rows}</Badge>
              <Badge variant="success">Thành công: {result.succeeded.count}</Badge>
              <Badge variant="error">Lỗi: {result.failed.count}</Badge>
              <Badge variant="warning">Trùng, bỏ qua: {result.skipped_duplicate.count}</Badge>
            </div>

            <Tabs defaultValue="failed">
              <TabsList>
                <TabsTrigger value="failed">Lỗi ({result.failed.count})</TabsTrigger>
                <TabsTrigger value="skipped">
                  Trùng ({result.skipped_duplicate.count})
                </TabsTrigger>
                <TabsTrigger value="succeeded">
                  Thành công ({result.succeeded.count})
                </TabsTrigger>
              </TabsList>

              <TabsContent value="failed" className="mt-3">
                <div className="max-h-64 overflow-y-auto rounded-md border border-hairline">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Dòng</TableHead>
                        <TableHead>Server ID</TableHead>
                        <TableHead>Lý do</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {result.failed.items.map((r, i) => (
                        <TableRow key={`${r.row}-${i}`}>
                          <TableCell className="font-mono">{r.row}</TableCell>
                          <TableCell className="font-mono text-ink">{r.server_id}</TableCell>
                          <TableCell className="font-mono text-error-deep">{r.reason}</TableCell>
                        </TableRow>
                      ))}
                      {!result.failed.items.length ? (
                        <TableRow>
                          <TableCell colSpan={3} className="text-center text-mute">
                            Không có dòng lỗi
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </TableBody>
                  </Table>
                </div>
              </TabsContent>

              <TabsContent value="skipped" className="mt-3">
                <IdList
                  items={result.skipped_duplicate.items}
                  empty="Không có dòng trùng"
                />
              </TabsContent>

              <TabsContent value="succeeded" className="mt-3">
                <IdList items={result.succeeded.items} empty="Không có dòng nào được nhập" />
              </TabsContent>
            </Tabs>

            <div className="flex justify-end">
              <Button onClick={close}>Đóng</Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
