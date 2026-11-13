-- Migration 042: stop duplicating plugin commands.
--
-- SyncDetectedPlugins inserted every detected plugin command with
-- ON CONFLICT DO NOTHING, but game_commands had no unique constraint, so
-- each plugin scan (every 5 minutes) added another copy of every command.
-- Remove the copies and add the unique index the insert can target.

DELETE FROM game_commands a
 USING game_commands b
 WHERE a.source = 'plugin'
   AND b.source = 'plugin'
   AND a.server_id = b.server_id
   AND a.command_template = b.command_template
   AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS uq_game_commands_plugin
    ON game_commands (server_id, command_template)
 WHERE source = 'plugin';
