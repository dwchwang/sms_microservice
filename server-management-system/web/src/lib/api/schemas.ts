import { z } from "zod";

const ipv4 = z
  .string()
  .regex(
    /^((25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(25[0-5]|2[0-4]\d|1?\d?\d)$/,
    "IPv4 phải có dạng x.x.x.x",
  );

const tcpPort = z.coerce
  .number()
  .int()
  .min(1, "Cổng từ 1 đến 65535")
  .max(65535, "Cổng từ 1 đến 65535");

export const loginSchema = z.object({
  email: z.string().email("Email không hợp lệ"),
  password: z.string().min(1, "Bắt buộc"),
});
export type LoginInput = z.infer<typeof loginSchema>;

export const registerSchema = z.object({
  email: z.string().email("Email không hợp lệ"),
  password: z.string().min(8, "Mật khẩu tối thiểu 8 ký tự"),
  full_name: z.string().min(1, "Bắt buộc"),
});
export type RegisterInput = z.infer<typeof registerSchema>;

// "" must become undefined so the field is omitted rather than sent as 0:
// the columns are CHECK (NULL OR > 0).
const optionalPositiveInt = z
  .union([z.coerce.number(), z.literal("")])
  .optional()
  .transform((v) => (v === "" || v === undefined ? undefined : Number(v)))
  .pipe(z.number().int().positive("Phải lớn hơn 0").optional());

export const createServerSchema = z.object({
  server_id: z
    .string()
    .min(3, "Tối thiểu 3 ký tự")
    .max(100)
    .regex(/^[A-Z0-9\-_]+$/, "Chỉ chữ HOA, số, - và _"),
  server_name: z.string().min(3, "Tối thiểu 3 ký tự").max(255),
  ipv4,
  tcp_port: tcpPort,
  os: z.string().optional(),
  cpu_cores: optionalPositiveInt,
  ram_gb: optionalPositiveInt,
  disk_gb: optionalPositiveInt,
  location: z.string().optional(),
  description: z.string().optional(),
});
export type CreateServerInput = z.infer<typeof createServerSchema>;

// server_id and status are absent on purpose: server_id cannot change, and status
// comes only from monitoring.
export const updateServerSchema = z.object({
  server_name: z.string().min(3).max(255).optional(),
  ipv4: ipv4.optional(),
  tcp_port: tcpPort.optional(),
  os: z.string().optional(),
  cpu_cores: optionalPositiveInt,
  ram_gb: optionalPositiveInt,
  disk_gb: optionalPositiveInt,
  location: z.string().optional(),
  description: z.string().optional(),
});
export type UpdateServerInput = z.infer<typeof updateServerSchema>;

export const sendReportSchema = z.object({
  start_date: z.string().min(1, "Bắt buộc"),
  end_date: z.string().min(1, "Bắt buộc"),
  recipient_email: z.string().email("Email không hợp lệ"),
});
export type SendReportInput = z.infer<typeof sendReportSchema>;

export const updateRoleSchema = z.object({
  role_name: z.enum(["admin", "operator", "viewer"]),
});
export type UpdateRoleInput = z.infer<typeof updateRoleSchema>;
