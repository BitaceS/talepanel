-- 037_server_last_exit_code.sql — remember why a server last crashed
--
-- The daemon reports the process exit code with a "crashed" status. Hytale's
-- exit codes 8 (staged self-update) and 11 (install-dir conflict) mean the
-- server files need a reinstall; the panel shows a Reinstall notice for them.
-- NULL = no unexpected exit since the last start/reinstall.

ALTER TABLE servers ADD COLUMN IF NOT EXISTS last_exit_code INTEGER;
