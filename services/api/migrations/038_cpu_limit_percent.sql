-- Migration 038: servers.cpu_limit is a percentage of one CPU core
-- (100 = one full core) and 0 = unlimited. The daemon now enforces it.
--
-- Until now the API stored 4 whenever the create form left the field empty
-- (the "CPU limit (%)" value was never acted on). Read as a percentage that
-- would throttle a server to 4% of a core, so reset that implicit default to
-- unlimited, which is what those servers have been running with all along.

UPDATE servers SET cpu_limit = 0 WHERE cpu_limit IS NULL OR cpu_limit = 4;
