-- Migration: report-service HA & scale-out (phase-8-report-ha-scale.md)
--
-- init.sql chỉ chạy khi volume postgres còn trống, nên cụm đang sống phải chạy
-- file này thủ công:
--
--   docker exec -i <postgres_container> \
--     psql -U report_user_v2 -d report_db < migrate_report_ha.sql
--
-- Idempotent: chạy lại nhiều lần không hỏng.

\c report_db;

-- CẢNH BÁO: lệnh dưới sẽ FAIL nếu report_jobs đang có bản ghi trùng
-- (requester_id, idempotency_key). Kiểm tra trước bằng:
--
--   SELECT requester_id, idempotency_key, COUNT(*)
--   FROM report_jobs WHERE idempotency_key <> ''
--   GROUP BY 1, 2 HAVING COUNT(*) > 1;
--
-- Nếu có, giữ lại row mới nhất mỗi nhóm rồi xoá phần còn lại trước khi chạy tiếp.
CREATE UNIQUE INDEX IF NOT EXISTS ux_report_jobs_idem
    ON report_jobs (requester_id, idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS cron_runs (
    job_name      VARCHAR(50)   NOT NULL,
    run_date      DATE          NOT NULL,
    state         VARCHAR(20)   NOT NULL CHECK (state IN ('running','done','failed')),
    owner         VARCHAR(255)  NOT NULL,
    started_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    heartbeat_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    error_message TEXT,
    PRIMARY KEY (job_name, run_date)
);
