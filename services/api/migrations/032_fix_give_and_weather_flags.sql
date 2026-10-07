-- Migration 032: fix two more default templates against HytaleServer.jar 0.6.8.
--
--   * `give {player} {item} {count}` -> quantity is an optional (default) arg,
--     so Hytale only accepts it as the --quantity flag; a third positional
--     fails with "too many arguments".
--   * `weather set clear` -> WeatherSetCommand is an AbstractWorldCommand like
--     `time set`, so from the console it needs --world.
--
-- Verified with javap against com.hypixel.hytale:Server:0.6.8 (maven.hytale.com).
-- Only rows still holding the old default are touched, so operator
-- customisations are preserved.

UPDATE game_commands SET command_template = 'give {player} {item} --quantity {count}'
  WHERE is_default = true AND command_template = 'give {player} {item} {count}';

UPDATE game_commands SET command_template = 'weather set clear --world {world}'
  WHERE is_default = true AND command_template = 'weather set clear';
