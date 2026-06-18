import { api, unwrap } from "./client";
import type {
  ImportJobResponse,
  ImportJobStatusResponse,
  MonitorStatus,
  ReportSummary,
  SendReportResponse,
  ServerListResponse,
  ServerResponse,
  TokenResponse,
  UserListResponse,
  UserProfile,
} from "./types";
import type {
  CreateServerInput,
  LoginInput,
  RegisterInput,
  SendReportInput,
  UpdateServerInput,
} from "./schemas";

type Env<T> = { data: T };

// ── Auth ──
export const authApi = {
  login: (body: LoginInput) =>
    api.post<Env<TokenResponse>>("/auth/login", body).then((r) => unwrap(r.data)),
  register: (body: RegisterInput) =>
    api.post<Env<UserProfile>>("/auth/register", body).then((r) => unwrap(r.data)),
  logout: () => api.post("/auth/logout").then((r) => r.data),
  profile: () =>
    api.get<Env<UserProfile>>("/auth/profile").then((r) => unwrap(r.data)),
};

// ── Servers ──
export interface ServerListParams {
  status?: "on" | "off";
  server_id?: string;
  server_name?: string;
  ipv4?: string;
  location?: string;
  os?: string;
  sort_by?: string;
  sort_order?: "asc" | "desc";
  page?: number;
  page_size?: number;
}

export const serverApi = {
  list: (params: ServerListParams) =>
    api
      .get<Env<ServerListResponse>>("/servers", { params })
      .then((r) => unwrap(r.data)),
  get: (serverId: string) =>
    api.get<Env<ServerResponse>>(`/servers/${serverId}`).then((r) => unwrap(r.data)),
  create: (body: CreateServerInput) =>
    api.post<Env<ServerResponse>>("/servers", body).then((r) => unwrap(r.data)),
  update: (serverId: string, body: UpdateServerInput) =>
    api.put<Env<ServerResponse>>(`/servers/${serverId}`, body).then((r) => unwrap(r.data)),
  remove: (serverId: string) =>
    api.delete<Env<{ server_id: string; message: string }>>(`/servers/${serverId}`).then((r) => unwrap(r.data)),
};

// ── Import / Export ──
export const fileApi = {
  importServers: (file: File) => {
    const form = new FormData();
    form.append("file", file);
    return api
      .post<Env<ImportJobResponse>>("/servers/import", form, {
        headers: { "Content-Type": "multipart/form-data" },
      })
      .then((r) => unwrap(r.data));
  },
  importStatus: (jobId: string) =>
    api
      .get<Env<ImportJobStatusResponse>>(`/servers/import/${jobId}`)
      .then((r) => unwrap(r.data)),
  exportServers: async (params: ServerListParams) => {
    const res = await api.post("/servers/export", params, { responseType: "blob" });
    // Filename from Content-Disposition if exposed, else generated.
    const cd: string = res.headers["content-disposition"] ?? "";
    const match = cd.match(/filename="?([^"]+)"?/);
    const filename =
      match?.[1] ??
      `servers_export_${new Date().toISOString().slice(0, 10).replace(/-/g, "")}.xlsx`;
    return { blob: res.data as Blob, filename };
  },
};

// ── Reports ──
export const reportApi = {
  summary: (start_date: string, end_date: string) =>
    api
      .get<Env<ReportSummary>>("/reports/summary", { params: { start_date, end_date } })
      .then((r) => unwrap(r.data)),
  send: (body: SendReportInput) =>
    api.post<Env<SendReportResponse>>("/reports", body).then((r) => unwrap(r.data)),
};

// ── Users ──
export const userApi = {
  list: (page: number, page_size = 20) =>
    api
      .get<Env<UserListResponse>>("/auth/users", { params: { page, page_size } })
      .then((r) => unwrap(r.data)),
  updateRole: (userId: string, role_name: string) =>
    api
      .put<Env<UserProfile>>(`/auth/users/${userId}/role`, { role_name })
      .then((r) => unwrap(r.data)),
};

// ── Monitor ──
export const monitorApi = {
  status: () =>
    api.get<Env<MonitorStatus>>("/monitor/status").then((r) => unwrap(r.data)),
};
