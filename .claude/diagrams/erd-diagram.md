# 🗄️ ERD — Entity Relationship Diagram

> **Ngày tạo:** 09/06/2026
> **Mô tả:** Sơ đồ quan hệ thực thể toàn hệ thống VCS-SMS (5 Schemas).

---

## Toàn cảnh ERD

```mermaid
erDiagram
    %% ═══════════════════════════════════
    %% auth_schema
    %% ═══════════════════════════════════
    USERS ||--o{ ROLES : "belongs to"
    ROLES ||--o{ ROLE_PERMISSIONS : "has many"

    USERS {
        uuid id PK
        varchar username UK
        varchar email UK
        varchar password_hash
        varchar full_name
        uuid role_id FK
        boolean is_active
        timestamptz last_login_at
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    ROLES {
        uuid id PK
        varchar name UK
        text description
        timestamptz created_at
        timestamptz updated_at
    }

    ROLE_PERMISSIONS {
        uuid id PK
        uuid role_id FK
        varchar scope
        timestamptz created_at
    }

    %% ═══════════════════════════════════
    %% server_schema
    %% ═══════════════════════════════════
    SERVERS {
        uuid id PK
        varchar server_id UK "SRV-00001"
        varchar server_name UK "web-0001"
        varchar status "on / off"
        varchar ipv4 "tcp-simulator"
        varchar os "Ubuntu 22.04"
        integer cpu_cores 8
        decimal ram_gb 16
        decimal disk_gb 500
        varchar location "DC-HN"
        text description
        timestamptz created_at
        timestamptz updated_at
        timestamptz deleted_at
    }

    %% ═══════════════════════════════════
    %% monitor_schema
    %% ═══════════════════════════════════
    HEALTH_CHECK_CONFIGS {
        uuid id PK
        varchar server_id UK "FK logic"
        varchar check_method "tcp"
        integer tcp_port "9000+index"
        integer tcp_timeout_ms 5000
        decimal uptime_rate "0.50-0.99"
        boolean is_enabled
        timestamptz created_at
        timestamptz updated_at
    }

    %% ═══════════════════════════════════
    %% report_schema
    %% ═══════════════════════════════════
    REPORT_JOBS {
        uuid id PK
        varchar report_type "daily / on_demand"
        varchar status "pending/completed/failed"
        date start_date
        date end_date
        varchar recipient_email
        integer total_servers
        integer servers_on
        integer servers_off
        decimal avg_uptime_pct
        text error_message
        timestamptz sent_at
        timestamptz created_at
    }

    DAILY_SNAPSHOTS {
        uuid id PK
        date snapshot_date UK
        integer total_servers
        integer servers_on
        integer servers_off
        decimal avg_uptime_pct
        jsonb low_uptime_servers
        timestamptz created_at
    }

    %% ═══════════════════════════════════
    %% fileio_schema
    %% ═══════════════════════════════════
    IMPORT_JOBS {
        uuid id PK
        varchar status "pending/processing/completed/failed"
        varchar file_name
        varchar file_path
        integer total_rows
        integer success_count
        integer failed_count
        text error_message
        uuid created_by
        timestamptz started_at
        timestamptz completed_at
        timestamptz created_at
    }

    IMPORT_JOB_DETAILS {
        uuid id PK
        uuid import_job_id FK
        integer row_number
        varchar server_id
        varchar server_name
        varchar status "success / failed"
        text error_reason
        timestamptz created_at
    }

    %% ═══════════════════════════════════
    %% Cross-schema relationships
    %% ═══════════════════════════════════
    SERVERS ||--o| HEALTH_CHECK_CONFIGS : "has config"
    IMPORT_JOBS ||--o{ IMPORT_JOB_DETAILS : "has details"
```

---

## Schema Ownership & Cross-Schema Access

| Schema | Owner Service | Tables | Read By | Write By |
|--------|:---:|---------|----------|----------|
| `auth_schema` | Auth | users, roles, role_permissions | (none) | Auth |
| `server_schema` | Server | servers | Monitor, Report, FileIO | Server, FileIO |
| `monitor_schema` | Monitor | health_check_configs | (none) | Monitor |
| `report_schema` | Report | report_jobs, daily_snapshots | (none) | Report |
| `fileio_schema` | FileIO | import_jobs, import_job_details | (none) | FileIO |

---

## Bảng Tóm Tắt — 10 Bảng

| # | Schema | Table | Records | Mục đích |
|---|--------|-------|---------|----------|
| 1 | auth_schema | `users` | ~10 | Quản lý tài khoản |
| 2 | auth_schema | `roles` | 3 | Admin, Operator, Viewer |
| 3 | auth_schema | `role_permissions` | ~9 | Scope cho từng role |
| 4 | server_schema | `servers` | 10,000 | Thông tin server |
| 5 | monitor_schema | `health_check_configs` | 10,000 | Cấu hình health-check |
| 6 | report_schema | `report_jobs` | variable | Lịch sử job báo cáo |
| 7 | report_schema | `daily_snapshots` | ~365/year | Snapshot uptime hàng ngày |
| 8 | fileio_schema | `import_jobs` | variable | Job import Excel |
| 9 | fileio_schema | `import_job_details` | variable | Chi tiết từng dòng import |
| 10 | (ES Index) | `server-status-logs` | 14.4M/day | Status logs cho uptime |

---

## Seed Data — Phân bố Uptime Rate

| Nhóm | Tỷ lệ | Uptime Range | Số lượng |
|------|--------|-------------|----------|
| 🟢 Tốt | 70% | 93%–99% | 7,000 servers |
| 🟡 Trung bình | 20% | 80%–93% | 2,000 servers |
| 🔴 Kém | 10% | 50%–80% | 1,000 servers |
