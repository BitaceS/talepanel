-- Hytale's `/op` requires subcommands `add`/`remove`/`self`. There is no
-- top-level `deop`. `/kick` and `/ban` accept exactly one positional arg
-- (the player); a free-form reason as a second positional arg fails with
-- "Expected: 1, actual: 2". Source: HytaleServer.jar 2026-03-26 + game8 docs.

-- Fix `op {player}` -> `op add {player}`.
UPDATE game_commands
SET command_template = 'op add {player}'
WHERE is_default = true AND command_template = 'op {player}';

-- Drop bogus `deop {player}` if present, then add the real `op remove {player}`.
DELETE FROM game_commands
WHERE is_default = true AND command_template = 'deop {player}';

INSERT INTO game_commands (category, name, command_template, is_default, sort_order, source)
SELECT 'Player', 'Deop Player', 'op remove {player}', true, 64, 'built-in'
WHERE NOT EXISTS (
  SELECT 1 FROM game_commands
  WHERE is_default = true AND command_template = 'op remove {player}'
);

-- Strip the `{reason}` second positional from kick/ban — the parser only
-- accepts one arg. Reason support would require `--reason "<text>"` and
-- proper quoting; leave it out of the default until verified.
UPDATE game_commands
SET command_template = 'kick {player}'
WHERE is_default = true AND command_template = 'kick {player} {reason}';

UPDATE game_commands
SET command_template = 'ban {player}'
WHERE is_default = true AND command_template = 'ban {player} {reason}';