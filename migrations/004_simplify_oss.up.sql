-- Simplify schema for pure open-source (remove multi-tenant tables)

-- Drop tenant-dependent tables first (foreign key order)
DROP TABLE IF EXISTS oauth_states;
DROP TABLE IF EXISTS tenant_secrets;
DROP TABLE IF EXISTS tenant_members;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;

-- Rename tenant_migrations to migrations and drop tenant_id
ALTER TABLE IF EXISTS tenant_migrations DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE IF EXISTS tenant_migrations DROP CONSTRAINT IF EXISTS tenant_migrations_tenant_id_cr_name_cr_namespace_key;
ALTER TABLE IF EXISTS tenant_migrations RENAME TO migrations;

-- Drop tenant_id from migration_presets
ALTER TABLE IF EXISTS migration_presets DROP COLUMN IF EXISTS tenant_id;

-- Add unique constraint on cr_name + cr_namespace (without tenant_id)
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'migrations_cr_name_cr_namespace_key') THEN
    ALTER TABLE migrations ADD CONSTRAINT migrations_cr_name_cr_namespace_key UNIQUE (cr_name, cr_namespace);
  END IF;
END $$;

-- Clean up old indexes
DROP INDEX IF EXISTS idx_tenant_members_tenant;
DROP INDEX IF EXISTS idx_tenant_members_user;
DROP INDEX IF EXISTS idx_tenant_secrets_tenant;
DROP INDEX IF EXISTS idx_tenant_migrations_tenant;
DROP INDEX IF EXISTS idx_oauth_states_expires;

-- Create new index without tenant scoping
CREATE INDEX IF NOT EXISTS idx_migrations_cr ON migrations(cr_name, cr_namespace);
CREATE INDEX IF NOT EXISTS idx_migrations_status ON migrations(status);
