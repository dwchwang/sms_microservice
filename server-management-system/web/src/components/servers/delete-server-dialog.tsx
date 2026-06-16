"use client";

import { toast } from "sonner";
import { useDeleteServer } from "@/lib/api/hooks";
import { errorMessage } from "@/lib/form";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/common/spinner";

export function DeleteServerDialog({
  serverId,
  serverName,
  open,
  onOpenChange,
  onDeleted,
}: {
  serverId: string;
  serverName?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
}) {
  const del = useDeleteServer();

  async function confirm() {
    try {
      await del.mutateAsync(serverId);
      toast.success("Đã xoá server");
      onOpenChange(false);
      onDeleted?.();
    } catch (err) {
      toast.error(errorMessage(err, "Xoá thất bại"));
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Xoá server?</DialogTitle>
          <DialogDescription>
            Server <span className="font-mono text-ink">{serverId}</span>
            {serverName ? ` (${serverName})` : ""} sẽ bị xoá. Hành động này không thể hoàn tác.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Huỷ
          </Button>
          <Button variant="destructive" onClick={confirm} disabled={del.isPending}>
            {del.isPending ? <Spinner /> : null}
            Xoá
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
