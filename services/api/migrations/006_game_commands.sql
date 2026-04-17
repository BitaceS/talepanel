-- 006_game_commands.sql — Predefined game commands for the Game Control panel

CREATE TABLE IF NOT EXISTS game_commands (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       UUID REFERENCES servers(id) ON DELETE CASCADE,
    category        VARCHAR(100) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    description     TEXT DEFAULT '',
    command_template VARCHAR(500) NOT NULL,
    icon            VARCHAR(50) DEFAULT '',
    params          JSONB DEFAULT '[]'::jsonb,
    sort_order      INT DEFAULT 0,
    is_default      BOOLEAN DEFAULT false,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- server_id NULL = global defaults available to all servers
CREATE INDEX idx_game_commands_server ON game_commands(server_id);
CREATE INDEX idx_game_commands_category ON game_commands(category);
