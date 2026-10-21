-- Migration 034: give the global built-in commands (server_id NULL, added by
-- migrations 022/023 after min_role existed) the same min_role as the
-- per-server defaults. They were left at the column default 'user', which
-- let any console user run stop / op / give through /game-commands/execute.
-- Only raises roles, never lowers one an admin already tightened.

UPDATE game_commands SET min_role = 'admin'
WHERE server_id IS NULL AND is_default = true
  AND min_role IN ('user', 'moderator')
  AND (command_template IN ('stop', 'reload')
       OR command_template LIKE 'op %'
       OR command_template LIKE 'give %'
       OR command_template LIKE 'gamemode %');

UPDATE game_commands SET min_role = 'moderator'
WHERE server_id IS NULL AND is_default = true
  AND min_role = 'user'
  AND (command_template LIKE 'kick %'
       OR command_template LIKE 'ban %'
       OR command_template LIKE 'unban %'
       OR command_template LIKE 'whitelist %'
       OR command_template LIKE 'time set %'
       OR command_template LIKE 'weather %'
       OR command_template LIKE 'teleport %'
       OR command_template LIKE 'say %');
