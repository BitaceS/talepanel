-- 019_fix_default_game_commands.sql
-- The original seed used Minecraft-style command syntax. Hytale's console
-- uses `save` (not `save-all`) and does NOT have `reload` or `weather` as
-- top-level commands. Drop the wrong rows and correct save-all → save.

UPDATE game_commands
   SET command_template = 'save'
 WHERE is_default = true
   AND command_template = 'save-all';

DELETE FROM game_commands
 WHERE is_default = true
   AND command_template IN ('reload', 'weather clear', 'weather rain');

-- Add gamemode + give if not already present (idempotent on re-run).
INSERT INTO game_commands (server_id, category, name, description, command_template, icon, params, sort_order, is_default, min_role)
SELECT s.id, 'World Management', 'Set Gamemode',
       'Change a player''s gamemode',
       'gamemode {mode} {player}', 'gamepad-2',
       '[{"name":"mode","type":"string","required":true,"placeholder":"survival|creative|adventure|spectator"},{"name":"player","type":"string","required":true,"placeholder":"Player name"}]'::jsonb,
       6, true, 'admin'
  FROM servers s
 WHERE NOT EXISTS (
   SELECT 1 FROM game_commands g
    WHERE g.server_id = s.id
      AND g.command_template = 'gamemode {mode} {player}'
 );

INSERT INTO game_commands (server_id, category, name, description, command_template, icon, params, sort_order, is_default, min_role)
SELECT s.id, 'Player Management', 'Give Item',
       'Give an item to a player',
       'give {player} {item} {count}', 'package',
       '[{"name":"player","type":"string","required":true,"placeholder":"Player name"},{"name":"item","type":"string","required":true,"placeholder":"item id"},{"name":"count","type":"number","required":false,"placeholder":"1"}]'::jsonb,
       8, true, 'admin'
  FROM servers s
 WHERE NOT EXISTS (
   SELECT 1 FROM game_commands g
    WHERE g.server_id = s.id
      AND g.command_template = 'give {player} {item} {count}'
 );
