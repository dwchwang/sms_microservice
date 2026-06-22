-- Make server_id / server_name uniqueness apply only to non-deleted rows so a
-- soft-deleted server can be re-imported. The table-level UNIQUE constraints
-- covered ALL rows (including soft-deleted ones), which blocked re-import.

ALTER TABLE server_schema.servers DROP CONSTRAINT IF EXISTS servers_server_id_key;
ALTER TABLE server_schema.servers DROP CONSTRAINT IF EXISTS servers_server_name_key;

-- Replace the plain (non-unique) partial indexes with UNIQUE partial indexes.
DROP INDEX IF EXISTS server_schema.idx_servers_server_id;
DROP INDEX IF EXISTS server_schema.idx_servers_server_name;

CREATE UNIQUE INDEX IF NOT EXISTS uq_servers_server_id
    ON server_schema.servers(server_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_servers_server_name
    ON server_schema.servers(server_name) WHERE deleted_at IS NULL;
