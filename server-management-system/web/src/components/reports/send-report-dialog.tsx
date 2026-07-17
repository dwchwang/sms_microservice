"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { useSendReport } from "@/lib/api/hooks";
import { sendReportSchema, type SendReportInput } from "@/lib/api/schemas";
import { handleFormError } from "@/lib/form";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Spinner } from "@/components/common/spinner";

export function SendReportDialog({
  open,
  onOpenChange,
  defaultStart,
  defaultEnd,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultStart: string;
  defaultEnd: string;
}) {
  const send = useSendReport();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<SendReportInput>({
    resolver: zodResolver(sendReportSchema),
    values: { start_date: defaultStart, end_date: defaultEnd, recipient_email: "" },
  });

  async function onSubmit(values: SendReportInput) {
    try {
      const res = await send.mutateAsync(values);
      const id = res.id.slice(0, 8);
      // delivery_unknown is neither success nor failure: the body was already on
      // the wire when the error hit, so nobody knows whether it arrived.
      if (res.state === "sent") {
        toast.success(`Đã gửi báo cáo tới ${res.recipient_email} (job ${id})`);
      } else if (res.state === "delivery_unknown") {
        toast.warning(
          `Không rõ mail đã tới hay chưa (job ${id}). Kiểm tra hộp thư Sent trước khi gửi lại.`,
        );
      } else {
        toast.error(res.error_message ?? `Gửi thất bại (job ${id})`);
      }
      onOpenChange(false);
    } catch (err) {
      handleFormError(err, setError);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Gửi báo cáo qua email</DialogTitle>
          <DialogDescription>Báo cáo uptime trong khoảng thời gian sẽ được gửi tới email.</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="Từ ngày" error={errors.start_date?.message} required>
              <Input type="date" {...register("start_date")} />
            </Field>
            <Field label="Đến ngày" error={errors.end_date?.message} required>
              <Input type="date" {...register("end_date")} />
            </Field>
          </div>
          <Field label="Email nhận" error={errors.recipient_email?.message} required>
            <Input type="email" placeholder="admin@company.com" {...register("recipient_email")} />
          </Field>
          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Huỷ
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? <Spinner /> : null}
              Gửi báo cáo
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
