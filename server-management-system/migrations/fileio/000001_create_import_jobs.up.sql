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
