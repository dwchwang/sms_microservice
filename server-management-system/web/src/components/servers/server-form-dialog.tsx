"use client";

import { useForm, useWatch, type Resolver } from "react-hook-form";
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
import { Switch } from "@/components/ui/switch";
import { Spinner } from "@/components/common/spinner";

type FormValues = CreateServerInput & { status?: "on" | "off" };

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
    setValue,
    control,
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
          os: server!.os ?? "",
          cpu_cores: server!.cpu_cores,
          ram_gb: server!.ram_gb,
          disk_gb: server!.disk_gb,
          location: server!.location ?? "",
          description: server!.description ?? "",
          status: server!.status,
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

  const status = useWatch({ control, name: "status" });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isEdit ? "Sửa server" : "Tạo server mới"}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? "Cập nhật thông tin. server_id không thể thay đổi."
              : "Nhập đầy đủ thông tin server."}
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
            <Field label="Hệ điều hành" error={errors.os?.message}>
              <Input {...register("os")} placeholder="Ubuntu 22.04" />
            </Field>
            <Field label="CPU cores" error={errors.cpu_cores?.message}>
              <Input type="number" {...register("cpu_cores")} placeholder="8" />
            </Field>
            <Field label="RAM (GB)" error={errors.ram_gb?.message}>
              <Input type="number" step="0.1" {...register("ram_gb")} placeholder="16" />
            </Field>
            <Field label="Disk (GB)" error={errors.disk_gb?.message}>
              <Input type="number" step="0.1" {...register("disk_gb")} placeholder="500" />
            </Field>
            <Field label="Vị trí" error={errors.location?.message}>
              <Input {...register("location")} placeholder="DC-HN" />
            </Field>
          </div>
          <Field label="Mô tả" error={errors.description?.message}>
            <Textarea {...register("description")} placeholder="Web server tầng frontend" />
          </Field>

          {isEdit ? (
            <div className="flex items-center gap-3">
              <Switch
                checked={status === "on"}
                onCheckedChange={(v) => setValue("status", v ? "on" : "off")}
              />
              <span className="text-sm text-ink">
                Trạng thái: <strong>{status === "on" ? "On" : "Off"}</strong>
              </span>
            </div>
          ) : null}

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
