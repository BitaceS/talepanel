-- Migration 036: the quick-setup installer mints the local node's enrollment
-- token with tale-cli before any user exists, so the creator is optional.

ALTER TABLE node_enrollments ALTER COLUMN created_by DROP NOT NULL;
