-- Revert to table-level UNIQUE constraints covering all rows.
DROP INDEX IF EXISTS server_schema.uq_servers_server_id;
DROP INDEX IF EXISTS server_schema.uq_servers_server_name;

ALTER TABLE server_schema.servers ADD CONSTRAINT servers_server_id_key UNIQUE (server_id);
ALTER TABLE server_schema.servers ADD CONSTRAINT servers_server_name_key UNIQUE (server_name);

CREATE INDEX IF NOT EXISTS idx_servers_server_id
    ON server_schema.servers(server_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_servers_server_name
    ON server_schema.servers(server_name) WHERE deleted_at IS NULL;
