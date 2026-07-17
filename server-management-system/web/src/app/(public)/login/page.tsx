"use client";

import { useRouter } from "next/navigation";
import Link from "next/link";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { authApi } from "@/lib/api/endpoints";
import { tokenStorage } from "@/store/auth";
import { useAuth } from "@/providers/auth-provider";
import { loginSchema, type LoginInput } from "@/lib/api/schemas";
import { handleFormError } from "@/lib/form";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/ui/field";
import { Spinner } from "@/components/common/spinner";
import { AuthShell } from "@/components/shell/auth-shell";

export default function LoginPage() {
  const router = useRouter();
  const { reload } = useAuth();
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<LoginInput>({ resolver: zodResolver(loginSchema) });

  async function onSubmit(values: LoginInput) {
    try {
      const tokens = await authApi.login(values);
      tokenStorage.set(tokens.access_token, tokens.refresh_token);
      await reload();
      toast.success("Đăng nhập thành công");
      router.replace("/");
    } catch (err) {
      handleFormError(err, setError);
    }
  }

  return (
    <AuthShell
      title="Đăng nhập."
      subtitle="Hệ thống quản lý 10.000 server VCS."
      footer={
        <>
          Chưa có tài khoản?{" "}
          <Link href="/register" className="text-link hover:underline">
            Đăng ký
          </Link>
        </>
      }
    >
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <Field label="Email" htmlFor="email" error={errors.email?.message} required>
          <Input id="email" type="email" autoComplete="email" {...register("email")} />
        </Field>
        <Field label="Mật khẩu" htmlFor="password" error={errors.password?.message} required>
          <Input id="password" type="password" autoComplete="current-password" {...register("password")} />
        </Field>
        <Button type="submit" className="w-full" disabled={isSubmitting}>
          {isSubmitting ? <Spinner /> : null}
          Đăng nhập
        </Button>
      </form>
    </AuthShell>
  );
}
