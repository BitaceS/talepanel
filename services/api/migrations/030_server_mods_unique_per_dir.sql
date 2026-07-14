-- 027 added mod_dir so a plugin could be toggled inside plugins/ instead of
-- mods/, but left the uniqueness key at (server_id, filename).
--
-- With mods/economy.jar AND plugins/economy.jar on the same server, both scan
-- results upsert onto the same row and mod_dir ends up whichever directory the
-- scanner happened to read last. Toggling then renames the file in the wrong
-- directory: the panel reports "disabled" while the still-enabled copy keeps
-- loading. The identity of a mod file is its path, not its name.
--
-- Deduplicate first (keep the newest row per (server_id, mod_dir, filename)),
-- then move the constraint.

DELETE FROM server_mods a
USING server_mods b
WHERE a.server_id = b.server_id
  AND a.mod_dir   = b.mod_dir
  AND a.filename  = b.filename
  AND a.ctid < b.ctid;

ALTER TABLE server_mods DROP CONSTRAINT IF EXISTS server_mods_server_id_filename_key;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'server_mods_server_id_mod_dir_filename_key'
    ) THEN
        ALTER TABLE server_mods
            ADD CONSTRAINT server_mods_server_id_mod_dir_filename_key
            UNIQUE (server_id, mod_dir, filename);
    END IF;
END $$;
