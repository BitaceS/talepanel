-- Migration 026: `world save --confirm` still fails with "specify a world with
-- --world or use --all". Save all worlds with --all. Supersedes migration 025's
-- value; matches both the original and the 025-migrated template.

UPDATE game_commands SET command_template = 'world save --confirm --all'
  WHERE is_default = true AND command_template IN ('world save', 'world save --confirm');
