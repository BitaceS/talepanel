-- 012_plugin_detection.sql — Automatic plugin detection metadata

-- ── Extend server_mods with detection metadata ──────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='source') THEN
        ALTER TABLE server_mods ADD COLUMN source TEXT DEFAULT 'manual';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='plugin_name') THEN
        ALTER TABLE server_mods ADD COLUMN plugin_name TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='author') THEN
        ALTER TABLE server_mods ADD COLUMN author TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='description') THEN
        ALTER TABLE server_mods ADD COLUMN description TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='detected_commands') THEN
        ALTER TABLE server_mods ADD COLUMN detected_commands JSONB DEFAULT '[]';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='config_files') THEN
        ALTER TABLE server_mods ADD COLUMN config_files JSONB DEFAULT '[]';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='file_hash') THEN
        ALTER TABLE server_mods ADD COLUMN file_hash TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='last_scanned_at') THEN
        ALTER TABLE server_mods ADD COLUMN last_scanned_at TIMESTAMPTZ;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='is_present') THEN
        ALTER TABLE server_mods ADD COLUMN is_present BOOLEAN DEFAULT true;
    END IF;
END $$;

-- ── Extend game_commands with source tracking ───────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='game_commands' AND column_name='source') THEN
        ALTER TABLE game_commands ADD COLUMN source TEXT DEFAULT 'built-in';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='game_commands' AND column_name='source_plugin') THEN
        ALTER TABLE game_commands ADD COLUMN source_plugin TEXT;
    END IF;
END $$;
