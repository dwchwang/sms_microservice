"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { authApi } from "@/lib/api/endpoints";
import { registerSchema, type RegisterInput } from "@/lib/api/schemas";
import { handleFormError } from "@/lib/form";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Spinner } from "@/components/common/spinner";
import { AuthShell } from "@/components/shell/auth-shell";

export default function RegisterPage() {
  const router = useRouter();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<RegisterInput>({ resolver: zodResolver(registerSchema) });

  async function onSubmit(values: RegisterInput) {
    try {
      await authApi.register(values);
      toast.success("Đăng ký thành công! Tài khoản được cấp quyền Viewer. Vui lòng đăng nhập.");
      router.replace("/login");
    } catch (err) {
      handleFormError(err, setError);
    }
  }

  return (
    <AuthShell
      title="Tạo tài khoản."
      subtitle="Tài khoản mới mặc định có quyền Viewer (chỉ đọc)."
      footer={
        <>
          Đã có tài khoản?{" "}
          <Link href="/login" className="text-link hover:underline">
            Đăng nhập
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <Field label="Tên đăng nhập" htmlFor="username" error={errors.username?.message} required>
          <Input id="username" autoComplete="username" {...register("username")} />
        </Field>
        <Field label="Email" htmlFor="email" error={errors.email?.message} required>
          <Input id="email" type="email" autoComplete="email" {...register("email")} />
        </Field>
        <Field label="Họ và tên" htmlFor="full_name" error={errors.full_name?.message} required>
          <Input id="full_name" {...register("full_name")} />
        </Field>
        <Field
          label="Mật khẩu"
          htmlFor="password"
          error={errors.password?.message}
          hint="Tối thiểu 8 ký tự"
          required
        >
          <Input id="password" type="password" autoComplete="new-password" {...register("password")} />
        </Field>
        <Button type="submit" className="w-full" disabled={isSubmitting}>
          {isSubmitting ? <Spinner /> : null}
          Đăng ký
        </Button>
      </form>
    </AuthShell>
  );
}
