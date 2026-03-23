-- Reverse: restore tenant tables and columns (run 001 schema again after this)
ALTER TABLE IF EXISTS migrations RENAME TO tenant_migrations;
ALTER TABLE IF EXISTS tenant_migrations ADD COLUMN IF NOT EXISTS tenant_id UUID;
ALTER TABLE IF EXISTS migration_presets ADD COLUMN IF NOT EXISTS tenant_id UUID;
DROP INDEX IF EXISTS idx_migrations_cr;
DROP INDEX IF EXISTS idx_migrations_status;
