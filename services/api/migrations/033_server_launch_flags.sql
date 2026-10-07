-- Migration 033: per-server Hytale launch flags (e.g. --accept-early-plugins
-- for modded servers). Values are validated against an allowlist in the API
-- and again in the daemon; this column only stores the selection.

ALTER TABLE servers ADD COLUMN IF NOT EXISTS launch_flags TEXT[] NOT NULL DEFAULT '{}';
