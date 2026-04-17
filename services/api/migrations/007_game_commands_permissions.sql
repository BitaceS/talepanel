-- 007_game_commands_permissions.sql — Add role-based permissions to game commands

ALTER TABLE game_commands ADD COLUMN IF NOT EXISTS min_role VARCHAR(20) DEFAULT 'user' NOT NULL;

-- Update dangerous commands to require higher roles
UPDATE game_commands SET min_role = 'admin' WHERE command_template IN ('stop', 'reload');
UPDATE game_commands SET min_role = 'moderator' WHERE command_template LIKE 'ban %' OR command_template LIKE 'unban %' OR command_template LIKE 'op %' OR command_template LIKE 'deop %';
