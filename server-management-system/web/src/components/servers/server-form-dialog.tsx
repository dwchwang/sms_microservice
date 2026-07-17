"use client";

import { useForm, type Resolver } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { useCreateServer, useUpdateServer } from "@/lib/api/hooks";
import {
  createServerSchema,
  updateServerSchema,
  type CreateServerInput,
} from "@/lib/api/schemas";
import { handleFormError } from "@/lib/form";
import type { ServerResponse } from "@/lib/api/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Spinner } from "@/components/common/spinner";

// No status here: it comes only from monitoring, never from the client.
type FormValues = CreateServerInput;

export function ServerFormDialog({
  open,
  onOpenChange,
  server,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server?: ServerResponse | null;
}) {
  const isEdit = !!server;
  const create = useCreateServer();
  const update = useUpdateServer(server?.server_id ?? "");

  const {
    register,
    handleSubmit,
    setError,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    resolver: zodResolver(
      isEdit ? updateServerSchema : createServerSchema,
    ) as Resolver<FormValues>,
    values: isEdit
      ? {
          server_id: server!.server_id,
          server_name: server!.server_name,
          ipv4: server!.ipv4,
          tcp_port: server!.tcp_port,
          os: server!.os ?? "",
          cpu_cores: server!.cpu_cores,
          ram_gb: server!.ram_gb,
          disk_gb: server!.disk_gb,
          location: server!.location ?? "",
          description: server!.description ?? "",
        }
      : undefined,
  });

  async function onSubmit(values: FormValues) {
    try {
      if (isEdit) {
        const { server_id: _omit, ...payload } = values;
        void _omit;
        await update.mutateAsync(payload);
        toast.success("Đã cập nhật server");
      } else {
        await create.mutateAsync(values);
        toast.success("Đã tạo server");
      }
      reset();
      onOpenChange(false);
    } catch (err) {
      handleFormError(err, setError);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? "Sửa server" : "Tạo server mới"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Cập nhật thông tin. server_id không thể thay đổi."
              : "IPv4 phải nằm trong dải CIDR được phép."}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Server ID" error={errors.server_id?.message} required>
              <Input
                {...register("server_id")}
                disabled={isEdit}
                placeholder="SRV-NEW-001"
                className="font-mono"
              />
            </Field>
            <Field label="Tên server" error={errors.server_name?.message} required>
              <Input {...register("server_name")} placeholder="web-new-01" />
            </Field>
            <Field label="IPv4" error={errors.ipv4?.message} required>
              <Input {...register("ipv4")} placeholder="10.0.1.100" className="font-mono" />
            </Field>
            <Field
              label="Cổng TCP"
              error={errors.tcp_port?.message}
              hint="Cổng dùng để health check"
              required
            >
              <Input
                type="number"
                {...register("tcp_port")}
                placeholder="80"
                className="font-mono"
              />
            </Field>
            <Field label="Hệ điều hành" error={errors.os?.message}>
              <Input {...register("os")} placeholder="Ubuntu 22.04" />
            </Field>
            <Field label="CPU cores" error={errors.cpu_cores?.message}>
              <Input type="number" {...register("cpu_cores")} placeholder="8" />
            </Field>
            <Field label="RAM (GB)" error={errors.ram_gb?.message}>
              <Input type="number" {...register("ram_gb")} placeholder="16" />
            </Field>
            <Field label="Disk (GB)" error={errors.disk_gb?.message}>
              <Input type="number" {...register("disk_gb")} placeholder="500" />
            </Field>
            <Field label="Vị trí" error={errors.location?.message}>
              <Input {...register("location")} placeholder="DC-HN" />
            </Field>
          </div>
          <Field label="Mô tả" error={errors.description?.message}>
            <Textarea {...register("description")} placeholder="Web server tầng frontend" />
          </Field>

          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
              Huỷ
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? <Spinner /> : null}
              {isEdit ? "Lưu thay đổi" : "Tạo server"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
