-- Migration 025: fix default game-command templates that fail from the console.
--
-- Hytale's CommandManager rejects these when run from the server console:
--   * `world save`      -> requires the --confirm flag
--   * `time set day`    -> requires --world (the console is not a player)
--   * `time set night`  -> requires --world
--
-- The {world} placeholder is substituted with the server's active world at
-- execution time. Only rows still holding the old (broken) default value are
-- updated so operator customisations are preserved.

UPDATE game_commands SET command_template = 'world save --confirm'
  WHERE is_default = true AND command_template = 'world save';

UPDATE game_commands SET command_template = 'time set day --world {world}'
  WHERE is_default = true AND command_template = 'time set day';

UPDATE game_commands SET command_template = 'time set night --world {world}'
  WHERE is_default = true AND command_template = 'time set night';
