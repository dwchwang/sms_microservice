-- ============================================================
-- VCS-SMS Database Initialization — v2 (New Design)
-- 3 separate databases + DB users
-- ============================================================

-- ============================
-- Tạo 3 databases
-- ============================
CREATE DATABASE identity_db;
CREATE DATABASE server_db;
CREATE DATABASE report_db;

-- ============================
-- Tạo DB users
-- ============================
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'identity_user') THEN
        CREATE USER identity_user WITH PASSWORD 'identity_pass_secret';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'server_user_v2') THEN
        CREATE USER server_user_v2 WITH PASSWORD 'server_pass_secret_v2';
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'report_user_v2') THEN
        CREATE USER report_user_v2 WITH PASSWORD 'report_pass_secret_v2';
    END IF;
END
$$;

-- ============================
-- Grant permissions
-- ============================
GRANT ALL PRIVILEGES ON DATABASE identity_db TO identity_user;
GRANT ALL PRIVILEGES ON DATABASE server_db TO server_user_v2;
GRANT ALL PRIVILEGES ON DATABASE report_db TO report_user_v2;

-- ============================================================
-- identity_db tables
-- ============================================================
\c identity_db;

-- Grant schema usage
GRANT ALL ON SCHEMA public TO identity_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO identity_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO identity_user;

CREATE TABLE IF NOT EXISTS roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50)   NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           VARCHAR(100)  NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id         UUID          NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope           VARCHAR(100)  NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE(role_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id ON role_permissions(role_id);

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           VARCHAR(255)  NOT NULL UNIQUE,
    password_hash   VARCHAR(500)  NOT NULL,  -- Argon2id produces longer hashes
    full_name       VARCHAR(255),
    role_id         UUID          NOT NULL REFERENCES roles(id),
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id ON users(role_id);

-- Seed roles (thiết kế mới: viewer, operator, admin)
INSERT INTO roles (id, name, description) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'admin',    'Full access to all resources'),
    ('a0000000-0000-0000-0000-000000000002', 'operator', 'Can operate servers and reports'),
    ('a0000000-0000-0000-0000-000000000003', 'viewer',   'Read-only access')
ON CONFLICT (name) DO NOTHING;

-- Seed scopes MỚI (1:1 theo endpoint)
INSERT INTO permissions (scope, description) VALUES
    ('server:create',     'Create server'),
    ('server:list',       'List servers'),
    ('server:view',       'View server detail'),
    ('server:update',     'Update server'),
    ('server:delete',     'Delete server'),
    ('server:import',     'Import servers from Excel'),
    ('server:export',     'Export servers to Excel'),
    ('server:stats',      'View server stats'),
    ('report:view',       'View report summary'),
    ('report:send',       'Send report email'),
    ('report:view_detail','View report detail'),
    ('user:list',         'List users'),
    ('user:manage_role',  'Change user role')
ON CONFLICT (scope) DO NOTHING;

-- Seed role_permissions
-- Admin: tất cả scopes
INSERT INTO role_permissions (role_id, scope)
SELECT 'a0000000-0000-0000-0000-000000000001', scope FROM permissions
ON CONFLICT (role_id, scope) DO NOTHING;

-- Operator: Viewer + create/update/delete/import/export + report:send + report:view_detail
INSERT INTO role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000002', 'server:create'),
    ('a0000000-0000-0000-0000-000000000002', 'server:list'),
    ('a0000000-0000-0000-0000-000000000002', 'server:view'),
    ('a0000000-0000-0000-0000-000000000002', 'server:update'),
    ('a0000000-0000-0000-0000-000000000002', 'server:delete'),
    ('a0000000-0000-0000-0000-000000000002', 'server:import'),
    ('a0000000-0000-0000-0000-000000000002', 'server:export'),
    ('a0000000-0000-0000-0000-000000000002', 'server:stats'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view'),
    ('a0000000-0000-0000-0000-000000000002', 'report:send'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view_detail')
ON CONFLICT (role_id, scope) DO NOTHING;

-- Viewer: list + view + stats + report:view
INSERT INTO role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000003', 'server:list'),
    ('a0000000-0000-0000-0000-000000000003', 'server:view'),
    ('a0000000-0000-0000-0000-000000000003', 'server:stats'),
    ('a0000000-0000-0000-0000-000000000003', 'report:view')
ON CONFLICT (role_id, scope) DO NOTHING;

-- Seed default admin (password: Admin@123456 — Argon2id hash)
-- Tạm giữ bcrypt cho đến khi Identity service chuyển sang Argon2id
INSERT INTO users (id, email, password_hash, full_name, role_id, is_active)
VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'admin@vcs.com',
    '$2a$10$95QUyF2JLw7SJwUBrw80BO1BipqhRz7iQQF/TUlga.Z/ohFK9UlOi',
    'System Administrator',
    'a0000000-0000-0000-0000-000000000001',
    TRUE
) ON CONFLICT (email) DO NOTHING;


-- ============================================================
-- server_db tables
-- ============================================================
\c server_db;

GRANT ALL ON SCHEMA public TO server_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO server_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO server_user_v2;

CREATE TABLE IF NOT EXISTS servers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id            VARCHAR(100)  NOT NULL,
    server_name          VARCHAR(255)  NOT NULL,
    status               VARCHAR(20)   NOT NULL DEFAULT 'UNKNOWN'
                         CHECK (status IN ('ON', 'OFF', 'UNKNOWN')),
    status_changed_at    TIMESTAMPTZ,
    status_version       BIGINT        NOT NULL DEFAULT 0,
    last_status_event_id VARCHAR(255),
    ipv4                 INET          NOT NULL,
    tcp_port             INT           NOT NULL DEFAULT 80
                         CHECK (tcp_port BETWEEN 1 AND 65535),
    os                   VARCHAR(100),
    cpu_cores            INT           CHECK (cpu_cores IS NULL OR cpu_cores > 0),
    ram_gb               INT           CHECK (ram_gb IS NULL OR ram_gb > 0),
    disk_gb              INT           CHECK (disk_gb IS NULL OR disk_gb > 0),
    location             VARCHAR(255),
    description          TEXT,
    created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ
);

-- server_id unique toàn cục (kể cả deleted — bảo vệ lịch sử)
CREATE UNIQUE INDEX IF NOT EXISTS ux_servers_server_id
    ON servers (server_id);

-- server_name unique chỉ trên server active (cho phép tên trùng với server đã xóa)
CREATE UNIQUE INDEX IF NOT EXISTS ux_servers_active_name
    ON servers (server_name) WHERE deleted_at IS NULL;

-- Index phục vụ filter và GET /servers/stats
CREATE INDEX IF NOT EXISTS ix_servers_status
    ON servers (status) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS ix_servers_created_at
    ON servers (created_at) WHERE deleted_at IS NULL;

-- Idempotency table
CREATE TABLE IF NOT EXISTS api_idempotency (
    actor_id         VARCHAR(255)  NOT NULL,
    endpoint         VARCHAR(255)  NOT NULL,
    idempotency_key  VARCHAR(255)  NOT NULL,
    request_hash     VARCHAR(64)   NOT NULL,
    state            VARCHAR(20)   NOT NULL DEFAULT 'processing'
                     CHECK (state IN ('processing', 'completed', 'failed')),
    status_code      INT,
    response_body    JSONB,
    expires_at       TIMESTAMPTZ   NOT NULL,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (actor_id, endpoint, idempotency_key)
);

CREATE INDEX IF NOT EXISTS ix_idempotency_expires
    ON api_idempotency (expires_at);


-- ============================================================
-- report_db tables
-- ============================================================
\c report_db;

GRANT ALL ON SCHEMA public TO report_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO report_user_v2;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO report_user_v2;

CREATE TABLE IF NOT EXISTS report_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_type       VARCHAR(20)   NOT NULL CHECK (report_type IN ('daily', 'on-demand')),
    requester_id      VARCHAR(255),
    idempotency_key   VARCHAR(255),
    start_at          DATE          NOT NULL,
    end_at            DATE          NOT NULL,
    recipient_email   VARCHAR(255)  NOT NULL,
    state             VARCHAR(30)   NOT NULL DEFAULT 'processing'
                      CHECK (state IN ('processing','generated','sending','sent','failed','delivery_unknown')),
    response_json     JSONB,
    smtp_message_id   VARCHAR(255),
    error_message     TEXT,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    sent_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS ix_report_jobs_state ON report_jobs(state);
CREATE INDEX IF NOT EXISTS ix_report_jobs_created ON report_jobs(created_at);
CREATE INDEX IF NOT EXISTS ix_report_jobs_type ON report_jobs(report_type);

CREATE TABLE IF NOT EXISTS daily_snapshots (
    server_id        VARCHAR(100)  NOT NULL,
    date             DATE          NOT NULL,
    server_name      VARCHAR(255)  NOT NULL,
    on_checks        INT           NOT NULL DEFAULT 0,
    actual_checks    INT           NOT NULL DEFAULT 0,
    expected_checks  INT           NOT NULL DEFAULT 0,
    uptime_pct       NUMERIC(5,2),          -- NULL khi actual_checks = 0 (no_data)
    last_status      VARCHAR(10),           -- ON/OFF cuối ngày; NULL khi no_data
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (server_id, date)
);

CREATE INDEX IF NOT EXISTS ix_daily_snapshots_date_uptime
    ON daily_snapshots (date, uptime_pct);
