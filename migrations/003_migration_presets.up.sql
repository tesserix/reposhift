-- Migration presets table for reusable branch filter configurations
-- Presets can be system-wide (tenant_id IS NULL) or tenant-specific

CREATE TABLE IF NOT EXISTS migration_presets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    branch_filter_mode VARCHAR(20),
    branches TEXT[],
    settings JSONB DEFAULT '{}',
    is_system BOOLEAN DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_migration_presets_tenant ON migration_presets(tenant_id);
CREATE INDEX IF NOT EXISTS idx_migration_presets_system ON migration_presets(is_system) WHERE is_system = true;

-- Seed system presets
INSERT INTO migration_presets (tenant_id, name, description, branch_filter_mode, branches, settings, is_system)
VALUES
    (
        NULL,
        'Standard Exclusions',
        'Excludes common non-essential branches: dependabot updates, WIP features, temporary branches, and test branches',
        'exclude',
        ARRAY['dependabot/*', 'feature/wip-*', 'tmp/*', 'test/*'],
        '{"recommended": true}'::jsonb,
        true
    ),
    (
        NULL,
        'Production Only',
        'Includes only production-ready branches: main, master, and release branches',
        'include',
        ARRAY['main', 'master', 'release/*'],
        '{"recommended": true}'::jsonb,
        true
    )
ON CONFLICT DO NOTHING;
