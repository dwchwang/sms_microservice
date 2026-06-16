/*
  API types — mirror of backend DTOs (verified against Go source, not just OpenAPI).
  Response envelope: { status, code, message, data, meta }. `code` = HTTP status.
*/

export type Scope =
  | "server:create"
  | "server:read"
  | "server:update"
  | "server:delete"
  | "server:import"
  | "server:export"
  | "report:view"
  | "report:send"
  | "user:manage"
  | "monitor:view";

export type Role = "admin" | "operator" | "viewer";
export type ServerStatus = "on" | "off";

export interface Meta {
  request_id?: string;
  timestamp?: string;
}

export interface ApiEnvelope<T> {
  status: string;
  code: number; // HTTP status code (NOT a 5-digit code)
  message: string;
  data: T;
  meta?: Meta;
}

export interface FieldError {
  field: string;
  code: string; // e.g. INVALID_FORMAT
  message: string;
}

export interface ApiErrorEnvelope {
  status: string;
  code: number;
  message: string;
  errors?: FieldError[];
  meta?: Meta;
}

// ── Auth ──
export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  token_type: string;
}

export interface UserProfile {
  id: string;
  username: string;
  email: string;
  full_name: string;
  role: Role;
  scopes: Scope[];
  is_active: boolean;
  created_at: string;
  // NOTE: backend UserResponse does NOT include last_login_at.
}

export interface UserListResponse {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  items: UserProfile[]; // users list uses `items`
}

// ── Server ──
export interface ServerResponse {
  id: string;
  server_id: string;
  server_name: string;
  status: ServerStatus;
  ipv4: string;
  os?: string;
  cpu_cores?: number;
  ram_gb?: number;
  disk_gb?: number;
  location?: string;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface ServerListResponse {
  servers: ServerResponse[]; // ⚠ server list uses `servers`, not `items`
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}

// ── Import / Export ──
export type ImportStatus = "pending" | "processing" | "completed" | "failed";

export interface ImportJobResponse {
  job_id: string;
  status: ImportStatus;
  file_name: string;
  message: string;
}

export interface ImportJobStatusResponse {
  job_id: string;
  status: ImportStatus;
  file_name: string;
  total_rows?: number;
  success_count?: number;
  failed_count?: number;
  success_list?: { server_id: string; server_name: string }[];
  failed_list?: {
    row_number: number;
    server_id: string;
    server_name: string;
    error_reason: string;
  }[];
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  created_at?: string;
}

// ── Report ──
// NOTE: backend ReportSummaryResponse is FLAT (verified against report-service dto).
export interface ServerUptime {
  server_id: string;
  server_name: string;
  uptime_pct: number;
  total_checks: number;
  on_checks: number;
}

export interface ReportSummary {
  start_date: string;
  end_date: string;
  total_servers: number;
  servers_on: number;
  servers_off: number;
  avg_uptime_pct: number;
  total_checks: number;
  low_uptime_servers: ServerUptime[];
}

export interface SendReportResponse {
  report_id: string;
  status: string;
  message: string;
  summary?: ReportSummary;
}

// ── Monitor ──
export interface MonitorStatus {
  status: string;
  service: string;
  check_interval: string;
  worker_count: number;
  tcp_timeout_ms: number;
  elasticsearch: string[];
  index: string;
  redis_available: boolean;
}
