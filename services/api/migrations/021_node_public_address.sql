-- Separate the internal address (how the API reaches the daemon) from the
-- public address shown to players. In single-host installs the API reaches
-- the daemon via host.docker.internal while players need a real hostname.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS public_address TEXT;
