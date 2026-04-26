-- Replace the default game-command set with commands actually present in
-- HytaleServer.jar (Hypixel's Hytale 0.5.x). Verified against the class
-- list under com/hypixel/hytale/server/core/.../commands/.

-- Drop bogus defaults that don't exist in Hytale.
DELETE FROM game_commands
WHERE is_default = true
  AND command_template IN (
    'save',
    'list',
    'deop {player}',
    'tp {player} {x} {y} {z}'
  );

-- Fix Save World — actual command is "world save".
UPDATE game_commands
SET command_template = 'world save'
WHERE is_default = true AND name = 'Save World';

-- Fix Teleport — actual command is "teleport", not "tp".
INSERT INTO game_commands (category, name, command_template, is_default, sort_order, source)
SELECT 'Player', 'Teleport Player', 'teleport {player} {x} {y} {z}', true, 70, 'built-in'
WHERE NOT EXISTS (
  SELECT 1 FROM game_commands
  WHERE is_default = true AND command_template = 'teleport {player} {x} {y} {z}'
);

-- Insert real commands that we know exist (idempotent).
INSERT INTO game_commands (category, name, command_template, is_default, sort_order, source)
SELECT v.category, v.name, v.command_template, true, v.sort_order, 'built-in'
FROM (VALUES
  ('Server', 'Stop Server',     'stop',                              10),
  ('Server', 'Broadcast',       'say {message}',                     20),
  ('World',  'Save World',      'world save',                        30),
  ('World',  'Set Time Day',    'time set day',                      40),
  ('World',  'Set Time Night',  'time set night',                    41),
  ('World',  'Weather Clear',   'weather set clear',                 50),
  ('Player', 'Kick Player',     'kick {player} {reason}',            60),
  ('Player', 'Ban Player',      'ban {player} {reason}',             61),
  ('Player', 'Unban Player',    'unban {player}',                    62),
  ('Player', 'Op Player',       'op {player}',                       63),
  ('Player', 'Give Item',       'give {player} {item} {count}',      80),
  ('Player', 'Set Gamemode',    'gamemode {mode} {player}',          81),
  ('Whitelist', 'Whitelist Add',    'whitelist add {player}',         90),
  ('Whitelist', 'Whitelist Remove', 'whitelist remove {player}',      91)
) AS v(category, name, command_template, sort_order)
WHERE NOT EXISTS (
  SELECT 1 FROM game_commands g
  WHERE g.is_default = true
    AND g.command_template = v.command_template
);
