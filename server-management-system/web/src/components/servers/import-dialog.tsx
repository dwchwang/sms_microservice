"use client";

import { useState } from "react";
import { UploadCloud, FileSpreadsheet } from "lucide-react";
import { toast } from "sonner";
import { useImportServers, useImportJob } from "@/lib/api/hooks";
import { errorMessage } from "@/lib/form";
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

export function ImportDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [file, setFile] = useState<File | null>(null);
  const [jobId, setJobId] = useState<string | null>(null);
  const importMut = useImportServers();
  const job = useImportJob(jobId);

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
      setJobId(res.job_id);
      toast.success("Đã tải lên, đang xử lý...");
    } catch (err) {
      toast.error(errorMessage(err, "Import thất bại"));
    }
  }

  function close() {
    setFile(null);
    setJobId(null);
    onOpenChange(false);
  }

  const data = job.data;
  const done = data?.status === "completed" || data?.status === "failed";
  const progressLabel: Record<string, string> = {
    pending: "Đang chờ...",
    processing: "Đang xử lý...",
    completed: "Hoàn tất",
    failed: "Thất bại",
  };

  return (
    <Dialog open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import servers từ Excel</DialogTitle>
          <DialogDescription>
            File .xlsx ≤ {MAX_MB}MB. Các server_id / server_name trùng sẽ bị bỏ qua.
          </DialogDescription>
        </DialogHeader>

        {!jobId ? (
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
            <div className="flex items-center gap-3">
              {!done ? <Spinner className="text-link" /> : null}
              <span className="text-sm font-medium text-ink">
                {progressLabel[data?.status ?? "pending"]}
              </span>
            </div>

            {data?.error_message ? (
              <p className="rounded-sm bg-error-soft px-3 py-2 text-sm text-error-deep">
                {data.error_message}
              </p>
            ) : null}

            {done && data ? (
              <>
                <div className="flex flex-wrap gap-2 text-sm">
                  <Badge>Tổng: {data.total_rows ?? 0}</Badge>
                  <Badge variant="success">Thành công: {data.success_count ?? 0}</Badge>
                  <Badge variant="error">Thất bại: {data.failed_count ?? 0}</Badge>
                </div>

                <Tabs defaultValue="failed">
                  <TabsList>
                    <TabsTrigger value="failed">Lỗi ({data.failed_list?.length ?? 0})</TabsTrigger>
                    <TabsTrigger value="success">
                      Thành công ({data.success_list?.length ?? 0})
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
                          {(data.failed_list ?? []).map((r, i) => (
                            <TableRow key={i}>
                              <TableCell className="font-mono">{r.row_number}</TableCell>
                              <TableCell className="font-mono text-ink">{r.server_id}</TableCell>
                              <TableCell className="text-error-deep">{r.error_reason}</TableCell>
                            </TableRow>
                          ))}
                          {!data.failed_list?.length ? (
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

                  <TabsContent value="success" className="mt-3">
                    <div className="max-h-64 overflow-y-auto rounded-md border border-hairline">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>Server ID</TableHead>
                            <TableHead>Tên</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {(data.success_list ?? []).map((r, i) => (
                            <TableRow key={i}>
                              <TableCell className="font-mono text-ink">{r.server_id}</TableCell>
                              <TableCell>{r.server_name}</TableCell>
                            </TableRow>
                          ))}
                          {!data.success_list?.length ? (
                            <TableRow>
                              <TableCell colSpan={2} className="text-center text-mute">
                                Không có
                              </TableCell>
                            </TableRow>
                          ) : null}
                        </TableBody>
                      </Table>
                    </div>
                  </TabsContent>
                </Tabs>

                <div className="flex justify-end">
                  <Button onClick={close}>Đóng</Button>
                </div>
              </>
            ) : null}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
