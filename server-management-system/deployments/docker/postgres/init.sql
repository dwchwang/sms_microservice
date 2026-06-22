-- ============================================================
-- VCS-SMS Database Initialization
-- Tạo 5 schemas + DB users + GRANT permissions
-- ============================================================

\c vcs_sms;

-- ============================
-- Tạo 5 schemas
-- ============================
CREATE SCHEMA IF NOT EXISTS auth_schema;
CREATE SCHEMA IF NOT EXISTS server_schema;
CREATE SCHEMA IF NOT EXISTS monitor_schema;
CREATE SCHEMA IF NOT EXISTS report_schema;
CREATE SCHEMA IF NOT EXISTS fileio_schema;

-- ============================
-- Tạo DB users cho mỗi service
-- ============================
CREATE USER auth_user WITH PASSWORD 'auth_pass_secret';
CREATE USER server_user WITH PASSWORD 'server_pass_secret';
CREATE USER monitor_user WITH PASSWORD 'monitor_pass_secret';
CREATE USER report_user WITH PASSWORD 'report_pass_secret';
CREATE USER fileio_user WITH PASSWORD 'fileio_pass_secret';

-- ============================
-- GRANT quyền — mỗi user sở hữu schema riêng
-- ============================

-- Auth Service
GRANT ALL PRIVILEGES ON SCHEMA auth_schema TO auth_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA auth_schema TO auth_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA auth_schema GRANT ALL ON TABLES TO auth_user;

-- Server Service
GRANT ALL PRIVILEGES ON SCHEMA server_schema TO server_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA server_schema TO server_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA server_schema GRANT ALL ON TABLES TO server_user;

-- Monitor Service — sở hữu monitor_schema + READ trên server_schema
GRANT ALL PRIVILEGES ON SCHEMA monitor_schema TO monitor_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA monitor_schema GRANT ALL ON TABLES TO monitor_user;
GRANT USAGE ON SCHEMA server_schema TO monitor_user;
GRANT SELECT ON ALL TABLES IN SCHEMA server_schema TO monitor_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA server_schema GRANT SELECT ON TABLES TO monitor_user;

-- Report Service
GRANT ALL PRIVILEGES ON SCHEMA report_schema TO report_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA report_schema GRANT ALL ON TABLES TO report_user;
GRANT USAGE ON SCHEMA server_schema TO report_user;
GRANT SELECT ON ALL TABLES IN SCHEMA server_schema TO report_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA server_schema GRANT SELECT ON TABLES TO report_user;

-- FileIO Service — sở hữu fileio_schema + READ/WRITE trên server_schema
GRANT ALL PRIVILEGES ON SCHEMA fileio_schema TO fileio_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA fileio_schema GRANT ALL ON TABLES TO fileio_user;
GRANT USAGE ON SCHEMA server_schema TO fileio_user;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA server_schema TO fileio_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA server_schema GRANT SELECT, INSERT ON TABLES TO fileio_user;

-- ============================
-- auth_schema tables
-- ============================

CREATE TABLE IF NOT EXISTS auth_schema.roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(50)   NOT NULL UNIQUE,
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_schema.role_permissions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id         UUID          NOT NULL REFERENCES auth_schema.roles(id) ON DELETE CASCADE,
    scope           VARCHAR(100)  NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    UNIQUE(role_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_role_permissions_role_id
    ON auth_schema.role_permissions(role_id);

CREATE TABLE IF NOT EXISTS auth_schema.users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username        VARCHAR(100)  NOT NULL UNIQUE,
    email           VARCHAR(255)  NOT NULL UNIQUE,
    password_hash   VARCHAR(255)  NOT NULL,
    full_name       VARCHAR(255),
    role_id         UUID          NOT NULL REFERENCES auth_schema.roles(id),
    is_active       BOOLEAN       NOT NULL DEFAULT TRUE,
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_username
    ON auth_schema.users(username) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_email
    ON auth_schema.users(email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id
    ON auth_schema.users(role_id);

-- Seed roles
INSERT INTO auth_schema.roles (id, name, description) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'admin',    'Full access to all resources'),
    ('a0000000-0000-0000-0000-000000000002', 'operator', 'Can operate servers, reports, and monitor status'),
    ('a0000000-0000-0000-0000-000000000003', 'viewer',   'Read-only access with export permission')
ON CONFLICT (name) DO NOTHING;

-- Seed role permissions
INSERT INTO auth_schema.role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'server:create'),
    ('a0000000-0000-0000-0000-000000000001', 'server:read'),
    ('a0000000-0000-0000-0000-000000000001', 'server:update'),
    ('a0000000-0000-0000-0000-000000000001', 'server:delete'),
    ('a0000000-0000-0000-0000-000000000001', 'server:import'),
    ('a0000000-0000-0000-0000-000000000001', 'server:export'),
    ('a0000000-0000-0000-0000-000000000001', 'monitor:view'),
    ('a0000000-0000-0000-0000-000000000001', 'report:view'),
    ('a0000000-0000-0000-0000-000000000001', 'report:send'),
    ('a0000000-0000-0000-0000-000000000001', 'user:manage'),
    -- Operator permissions
    ('a0000000-0000-0000-0000-000000000002', 'server:create'),
    ('a0000000-0000-0000-0000-000000000002', 'server:read'),
    ('a0000000-0000-0000-0000-000000000002', 'server:update'),
    ('a0000000-0000-0000-0000-000000000002', 'server:import'),
    ('a0000000-0000-0000-0000-000000000002', 'server:export'),
    ('a0000000-0000-0000-0000-000000000002', 'monitor:view'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view'),
    ('a0000000-0000-0000-0000-000000000002', 'report:send'),
    -- Viewer permissions
    ('a0000000-0000-0000-0000-000000000003', 'server:read'),
    ('a0000000-0000-0000-0000-000000000003', 'server:export'),
    ('a0000000-0000-0000-0000-000000000003', 'report:view')
ON CONFLICT (role_id, scope) DO NOTHING;

-- Seed default admin account (password: Admin@123456)
INSERT INTO auth_schema.users (id, username, email, password_hash, full_name, role_id, is_active)
VALUES (
    'b0000000-0000-0000-0000-000000000001',
    'admin',
    'admin@vcs.com',
    '$2a$10$95QUyF2JLw7SJwUBrw80BO1BipqhRz7iQQF/TUlga.Z/ohFK9UlOi',
    'System Administrator',
    'a0000000-0000-0000-0000-000000000001',
    TRUE
) ON CONFLICT (username) DO NOTHING;

-- ============================
-- server_schema tables
-- ============================

CREATE TABLE IF NOT EXISTS server_schema.servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       VARCHAR(100)  NOT NULL,
    server_name     VARCHAR(255)  NOT NULL,
    status          VARCHAR(20)   NOT NULL DEFAULT 'off' CHECK (status IN ('on', 'off')),
    ipv4            VARCHAR(15)   NOT NULL,
    os              VARCHAR(100),
    cpu_cores       INTEGER       CHECK (cpu_cores > 0),
    ram_gb          DECIMAL(10,2) CHECK (ram_gb > 0),
    disk_gb         DECIMAL(10,2) CHECK (disk_gb > 0),
    location        VARCHAR(255),
    description     TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

-- Uniqueness applies only to non-deleted rows so a soft-deleted server can be re-imported.
CREATE UNIQUE INDEX IF NOT EXISTS uq_servers_server_id
    ON server_schema.servers(server_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_servers_server_name
    ON server_schema.servers(server_name) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_status
    ON server_schema.servers(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_ipv4
    ON server_schema.servers(ipv4) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_created_at
    ON server_schema.servers(created_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_status_created
    ON server_schema.servers(status, created_at DESC) WHERE deleted_at IS NULL;

-- ============================
-- monitor_schema tables
-- ============================

CREATE TABLE IF NOT EXISTS monitor_schema.health_check_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       VARCHAR(100)  NOT NULL UNIQUE,
    check_method    VARCHAR(20)   NOT NULL DEFAULT 'tcp' CHECK (check_method IN ('tcp', 'simulator')),
    tcp_port        INTEGER       DEFAULT 80,
    tcp_timeout_ms  INTEGER       DEFAULT 5000,
    uptime_rate     DECIMAL(3,2)  DEFAULT 0.95 CHECK (uptime_rate >= 0 AND uptime_rate <= 1),
    is_enabled      BOOLEAN       NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hc_configs_server_id
    ON monitor_schema.health_check_configs(server_id);
CREATE INDEX IF NOT EXISTS idx_hc_configs_enabled
    ON monitor_schema.health_check_configs(is_enabled) WHERE is_enabled = TRUE;

-- ============================
-- report_schema tables
-- ============================

CREATE TABLE IF NOT EXISTS report_schema.report_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    report_type     VARCHAR(20)   NOT NULL CHECK (report_type IN ('daily', 'on_demand')),
    status          VARCHAR(20)   NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    start_date      DATE          NOT NULL,
    end_date        DATE          NOT NULL,
    recipient_email VARCHAR(255)  NOT NULL,
    total_servers   INTEGER,
    servers_on      INTEGER,
    servers_off     INTEGER,
    avg_uptime_pct  DECIMAL(5,2),
    error_message   TEXT,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_report_jobs_type ON report_schema.report_jobs(report_type);
CREATE INDEX IF NOT EXISTS idx_report_jobs_status ON report_schema.report_jobs(status);
CREATE INDEX IF NOT EXISTS idx_report_jobs_created ON report_schema.report_jobs(created_at);

CREATE TABLE IF NOT EXISTS report_schema.daily_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_date   DATE          NOT NULL UNIQUE,
    total_servers   INTEGER       NOT NULL,
    servers_on      INTEGER       NOT NULL,
    servers_off     INTEGER       NOT NULL,
    avg_uptime_pct  DECIMAL(5,2)  NOT NULL,
    low_uptime_servers JSONB,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_snapshots_date
    ON report_schema.daily_snapshots(snapshot_date);

-- ============================
-- fileio_schema tables
-- ============================

CREATE TABLE IF NOT EXISTS fileio_schema.import_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status          VARCHAR(20)   NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    file_name       VARCHAR(255)  NOT NULL,
    file_path       VARCHAR(500)  NOT NULL,
    total_rows      INTEGER       DEFAULT 0,
    success_count   INTEGER       DEFAULT 0,
    failed_count    INTEGER       DEFAULT 0,
    error_message   TEXT,
    created_by      UUID,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_import_jobs_status ON fileio_schema.import_jobs(status);
CREATE INDEX IF NOT EXISTS idx_import_jobs_created_by ON fileio_schema.import_jobs(created_by);
CREATE INDEX IF NOT EXISTS idx_import_jobs_deleted_at ON fileio_schema.import_jobs(deleted_at);

CREATE TABLE IF NOT EXISTS fileio_schema.import_job_details (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_job_id   UUID          NOT NULL REFERENCES fileio_schema.import_jobs(id) ON DELETE CASCADE,
    row_number      INTEGER       NOT NULL,
    server_id       VARCHAR(100),
    server_name     VARCHAR(255),
    status          VARCHAR(20)   NOT NULL CHECK (status IN ('success', 'failed')),
    error_reason    TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_import_details_job_id ON fileio_schema.import_job_details(import_job_id);
CREATE INDEX IF NOT EXISTS idx_import_details_status ON fileio_schema.import_job_details(status);
