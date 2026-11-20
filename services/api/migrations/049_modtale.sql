-- Modtale (modtale.net) as a second mod source next to CurseForge. A Modtale
-- download link expires within minutes, so the row keeps the project ID and
-- the panel asks Modtale for a fresh link on every install or version switch.
ALTER TABLE server_mods ADD COLUMN IF NOT EXISTS modtale_project_id TEXT;
