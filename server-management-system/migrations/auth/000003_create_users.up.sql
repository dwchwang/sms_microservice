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

-- Seed default admin account (password: Admin@123456)
-- Role can only be managed by existing admins.
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
