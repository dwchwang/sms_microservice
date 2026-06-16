"use client";

import { useState } from "react";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { fileApi, type ServerListParams } from "@/lib/api/endpoints";
import { errorMessage } from "@/lib/form";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/common/spinner";

export function ExportButton({ params }: { params: ServerListParams }) {
  const [loading, setLoading] = useState(false);

  async function handleExport() {
    setLoading(true);
    try {
      const { blob, filename } = await fileApi.exportServers(params);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      toast.success("Đã tải file export");
    } catch (err) {
      toast.error(errorMessage(err, "Export thất bại"));
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button variant="secondary" onClick={handleExport} disabled={loading}>
      {loading ? <Spinner /> : <Download />}
      Export
    </Button>
  );
}
