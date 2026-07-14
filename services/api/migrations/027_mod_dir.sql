-- 027_mod_dir.sql — Track which directory a mod/plugin file lives in.
--
-- The daemon's scanner reports files from BOTH mods/ and plugins/ into
-- server_mods, but enable_mod/disable_mod always renamed inside mods/. Toggling
-- a plugin therefore silently did nothing. Persist the source directory so the
-- toggle command can carry it ("dir" in the daemon payload).
--
-- Existing rows default to 'mods', which is exactly the previous behaviour.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='server_mods' AND column_name='mod_dir') THEN
        ALTER TABLE server_mods ADD COLUMN mod_dir TEXT NOT NULL DEFAULT 'mods';
    END IF;
END $$;

-- The daemon only ever renames inside these two directories; keep the DB from
-- handing it anything else.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'server_mods_mod_dir_check'
    ) THEN
        ALTER TABLE server_mods
            ADD CONSTRAINT server_mods_mod_dir_check CHECK (mod_dir IN ('mods', 'plugins'));
    END IF;
END $$;
