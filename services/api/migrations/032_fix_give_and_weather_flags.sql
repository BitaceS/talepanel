-- Migration 032: fix two more default templates against HytaleServer.jar 0.6.8.
--
--   * `give {player} {item} {count}` -> quantity is an optional (default) arg,
--     so Hytale only accepts it as the --quantity flag; a third positional
--     fails with "too many arguments".
--   * `weather set clear` -> there is no 'clear' weather asset ("Failed to find
--     Weather: clear"); Zone1_Sunny is the clear-sky weather. WeatherSetCommand
--     is also an AbstractWorldCommand like
--     `time set`, so from the console it needs --world.
--
-- Verified with javap against com.hypixel.hytale:Server:0.6.8 and by running
-- each command on a live 0.6.8 console.
-- Only rows still holding the old default are touched, so operator
-- customisations are preserved.

UPDATE game_commands SET command_template = 'give {player} {item} --quantity {count}'
  WHERE is_default = true AND command_template = 'give {player} {item} {count}';

UPDATE game_commands SET command_template = 'weather set Zone1_Sunny --world {world}'
  WHERE is_default = true AND command_template = 'weather set clear';
