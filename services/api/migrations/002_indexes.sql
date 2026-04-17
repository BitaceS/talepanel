-- ─────────────────────────────────────────────────────────────────────────────
-- TalePanel — Migration 002: Performance Indexes
-- ─────────────────────────────────────────────────────────────────────────────

-- Users
CREATE INDEX idx_users_email      ON users(email);
CREATE INDEX idx_users_username   ON users(username);
CREATE INDEX idx_users_role       ON users(role);
CREATE INDEX idx_users_active     ON users(is_active) WHERE is_active = true;

-- Sessions
CREATE INDEX idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);
CREATE INDEX idx_sessions_expires    ON sessions(expires_at);
CREATE INDEX idx_sessions_active     ON sessions(user_id, revoked) WHERE revoked = false;

-- API Keys
CREATE INDEX idx_api_keys_user ON api_keys(user_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- Nodes
CREATE INDEX idx_nodes_status    ON nodes(status);
CREATE INDEX idx_nodes_heartbeat ON nodes(last_heartbeat DESC NULLS LAST);

-- Servers
CREATE INDEX idx_servers_node_id   ON servers(node_id);
CREATE INDEX idx_servers_owner_id  ON servers(owner_id);
CREATE INDEX idx_servers_status    ON servers(status);
CREATE INDEX idx_servers_created   ON servers(created_at DESC);

-- Server Members
CREATE INDEX idx_server_members_user ON server_members(user_id);

-- Worlds
CREATE INDEX idx_worlds_server   ON worlds(server_id);
CREATE INDEX idx_worlds_active   ON worlds(server_id, is_active) WHERE is_active = true;

-- Mods
CREATE INDEX idx_mods_server  ON mods(server_id);
CREATE INDEX idx_mods_enabled ON mods(server_id, enabled) WHERE enabled = true;

-- Players
CREATE INDEX idx_players_server      ON players(server_id);
CREATE INDEX idx_players_hytale_uuid ON players(hytale_uuid);
CREATE INDEX idx_players_banned      ON players(server_id, is_banned) WHERE is_banned = true;
CREATE INDEX idx_players_last_seen   ON players(last_seen DESC NULLS LAST);

-- Player Sessions
CREATE INDEX idx_player_sessions_player ON player_sessions(player_id);
CREATE INDEX idx_player_sessions_joined ON player_sessions(joined_at DESC);

-- Backups
CREATE INDEX idx_backups_server    ON backups(server_id);
CREATE INDEX idx_backups_status    ON backups(status);
CREATE INDEX idx_backups_created   ON backups(created_at DESC);
CREATE INDEX idx_backups_expires   ON backups(expires_at) WHERE expires_at IS NOT NULL;

-- Backup Schedules
CREATE INDEX idx_backup_schedules_server  ON backup_schedules(server_id);
CREATE INDEX idx_backup_schedules_next    ON backup_schedules(next_run) WHERE enabled = true;

-- Alert Rules
CREATE INDEX idx_alert_rules_user   ON alert_rules(user_id);
CREATE INDEX idx_alert_rules_server ON alert_rules(server_id);

-- Alert Events
CREATE INDEX idx_alert_events_server    ON alert_events(server_id);
CREATE INDEX idx_alert_events_node      ON alert_events(node_id);
CREATE INDEX idx_alert_events_unresolved ON alert_events(resolved) WHERE resolved = false;
CREATE INDEX idx_alert_events_created   ON alert_events(created_at DESC);

-- Activity Logs
CREATE INDEX idx_activity_user    ON activity_logs(user_id);
CREATE INDEX idx_activity_server  ON activity_logs(server_id);
CREATE INDEX idx_activity_action  ON activity_logs(action);
CREATE INDEX idx_activity_created ON activity_logs(created_at DESC);

-- Metrics Snapshots
CREATE INDEX idx_metrics_server  ON metrics_snapshots(server_id, recorded_at DESC);
CREATE INDEX idx_metrics_recent  ON metrics_snapshots(recorded_at DESC);
