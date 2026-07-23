import { api, unwrap } from "./client";
import type {
  ImportResponse,
  JobResponse,
  ReportSummary,
  ServerListResponse,
  ServerResponse,
  ServerStatus,
  StatsResponse,
  TokenResponse,
  UptimeResponse,
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

// crypto.randomUUID() only exists in a secure context (HTTPS or localhost). Served
// over plain HTTP on a LAN IP it is undefined, so fall back to getRandomValues,
// which is available everywhere, to build a v4 UUID.
function uuidV4(): string {
  const c = globalThis.crypto;
  if (c?.randomUUID) return c.randomUUID();
  const b = new Uint8Array(16);
  c.getRandomValues(b);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// POST /servers and /servers/import reject a request without this header.
function idempotencyKey(): Record<string, string> {
  return { "Idempotency-Key": uuidV4() };
}

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
  status?: ServerStatus;
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
  stats: () =>
    api.get<Env<StatsResponse>>("/servers/stats").then((r) => unwrap(r.data)),
  uptime: () =>
    api.get<Env<UptimeResponse>>("/servers/uptime").then((r) => unwrap(r.data)),
  create: (body: CreateServerInput) =>
    api
      .post<Env<ServerResponse>>("/servers", body, { headers: idempotencyKey() })
      .then((r) => unwrap(r.data)),
  update: (serverId: string, body: UpdateServerInput) =>
    api.put<Env<ServerResponse>>(`/servers/${serverId}`, body).then((r) => unwrap(r.data)),
  remove: (serverId: string) =>
    api
      .delete<Env<{ server_id: string; message: string }>>(`/servers/${serverId}`)
      .then((r) => unwrap(r.data)),
};

// ── Import / Export ──
export const fileApi = {
  // Synchronous: the response is the full report, there is no job to poll.
  importServers: (file: File) => {
    const form = new FormData();
    form.append("file", file);
    return api
      .post<Env<ImportResponse>>("/servers/import", form, {
        // The axios instance defaults Content-Type to application/json, which would
        // serialize the FormData to "{}". Override so the browser sends multipart
        // with the correct boundary.
        headers: { "Content-Type": "multipart/form-data", ...idempotencyKey() },
      })
      .then((r) => unwrap(r.data));
  },
  exportServers: async (params: ServerListParams) => {
    const res = await api.post("/servers/export", params, { responseType: "blob" });
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
    api.post<Env<JobResponse>>("/reports", body).then((r) => unwrap(r.data)),
  get: (id: string) =>
    api.get<Env<JobResponse>>(`/reports/${id}`).then((r) => unwrap(r.data)),
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
