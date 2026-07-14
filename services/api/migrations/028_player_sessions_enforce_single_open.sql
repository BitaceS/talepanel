-- Sessions and playtime were never written: player_sessions was only ever read
-- and playtime_s only ever displayed.  Recording them (services/player_service.go)
-- makes a duplicate join event dangerous — two open sessions for the same player
-- would double-count playtime forever.
--
-- Close whatever stale open sessions exist before the index can be created, then
-- enforce "at most one open session per player" in the schema rather than in
-- application logic alone.

UPDATE player_sessions s
SET left_at    = COALESCE(p.last_seen, s.joined_at),
    duration_s = GREATEST(0, EXTRACT(EPOCH FROM (COALESCE(p.last_seen, s.joined_at) - s.joined_at))::INT)
FROM players p
WHERE p.id = s.player_id
  AND s.left_at IS NULL;

-- Deduplicate any rows the statement above could not close (player row gone).
UPDATE player_sessions
SET left_at = joined_at, duration_s = 0
WHERE left_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_player_sessions_one_open
    ON player_sessions (player_id)
    WHERE left_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_player_sessions_player_joined
    ON player_sessions (player_id, joined_at DESC);
