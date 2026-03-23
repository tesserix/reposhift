-- Seed default system tenant for platform-level operations
INSERT INTO tenants (id, name, slug, plan, mode, settings)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'System',
    'system',
    'enterprise',
    'saas',
    '{"is_system": true, "description": "Default system tenant for platform-level operations"}'::jsonb
)
ON CONFLICT (slug) DO NOTHING;
