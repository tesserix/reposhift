-- Remove seed data (reverse order of dependencies)
DELETE FROM tenants WHERE slug = 'system' AND id = '00000000-0000-0000-0000-000000000001';
