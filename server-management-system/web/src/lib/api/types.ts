/*
  API types — mirror of backend DTOs (verified against Go source, not just OpenAPI).
  Response envelope: { status, code, message, data, meta }. `code` = HTTP status.
*/

// Scopes are enforced per endpoint by RequireScope in each service.
export type Scope =
  | "server:create"
  | "server:list"
  | "server:view"
  | "server:update"
  | "server:delete"
  | "server:import"
  | "server:export"
  | "server:stats"
  | "report:view"
  | "report:send"
  | "report:view_detail"
  | "user:list"
  | "user:manage_role";

export type Role = "admin" | "operator" | "viewer";

// UNKNOWN = never checked yet. Uppercase on the wire.
export type ServerStatus = "ON" | "OFF" | "UNKNOWN";

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
  email: string;
  full_name: string;
  role: Role;
  scopes: Scope[];
  is_active: boolean;
  created_at: string;
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
  server_id: string;
  server_name: string;
  status: ServerStatus;
  status_changed_at?: string;
  // Read fresh from Redis at serialize time; null when unavailable.
  last_status_check: string | null;
  ipv4: string;
  tcp_port: number;
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

export interface StatsResponse {
  total: number;
  on: number;
  off: number;
  unknown: number;
}

// ── Import / Export ──
// Import is synchronous: one request in, one full report out. No job polling.
export interface ImportFailedItem {
  row: number;
  server_id: string;
  reason: string;
}

export interface ImportResponse {
  total_rows: number;
  succeeded: { count: number; items: string[] };
  failed: { count: number; items: ImportFailedItem[] };
  skipped_duplicate: { count: number; items: string[] };
}

// ── Uptime (dashboard) ──
// Lifetime counters kept by Monitoring in Redis. No snapshot needed, so this is
// answerable at any moment.
export interface ServerUptime {
  server_id: string;
  server_name: string;
  uptime_pct: number;
  total_checks: number;
  on_checks: number;
}

export interface UptimeResponse {
  total_servers: number;
  servers_on: number;
  servers_off: number;
  servers_unknown: number;

  servers_no_data: number;
  servers_uptime_100: number;
  servers_uptime_partial: number;
  servers_uptime_0: number;

  avg_uptime_pct: number | null;
  top_10_lowest_uptime: ServerUptime[];
}

// ── Report (email / lịch sử, đọc daily_snapshots) ──
export interface LowUptimeServer {
  server_id: string;
  server_name: string;
  uptime_pct: number;
}

export interface ReportSummary {
  start_date: string;
  end_date: string;

  total_servers: number;
  // A snapshot at the end of the window, not a property of the whole period.
  servers_on_at_end_at: number;
  servers_off_at_end_at: number;

  servers_uptime_100: number;
  servers_uptime_partial: number;
  servers_uptime_0: number;
  servers_no_data: number;

  // null when no server in the window had any data.
  avg_uptime_pct: number | null;

  expected_checks: number;
  actual_checks: number;
  coverage_pct: number;
  degraded: boolean;

  top_10_lowest_uptime: LowUptimeServer[];
}

export type ReportState =
  | "processing"
  | "generated"
  | "sending"
  | "sent"
  | "failed"
  | "delivery_unknown";

export interface JobResponse {
  id: string;
  report_type: string;
  state: ReportState;
  start_date: string;
  end_date: string;
  recipient_email: string;
  smtp_message_id?: string;
  error_message?: string;
  summary?: ReportSummary;
  created_at: string;
  sent_at?: string;
}
