-- 011_server_databases.sql — MySQL database per server

CREATE TABLE IF NOT EXISTS server_databases (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   UUID    NOT NULL UNIQUE REFERENCES servers(id) ON DELETE CASCADE,
    db_name     TEXT    NOT NULL,
    db_user     TEXT    NOT NULL,
    db_password TEXT    NOT NULL,
    host        TEXT    NOT NULL DEFAULT 'mariadb',
    port        INTEGER NOT NULL DEFAULT 3306,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_server_databases_server_id ON server_databases(server_id);
