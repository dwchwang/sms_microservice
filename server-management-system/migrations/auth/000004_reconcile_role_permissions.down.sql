UPDATE auth_schema.roles
SET description = 'Can read and update servers, view reports',
    updated_at = NOW()
WHERE id = 'a0000000-0000-0000-0000-000000000002';

UPDATE auth_schema.roles
SET description = 'Read-only access',
    updated_at = NOW()
WHERE id = 'a0000000-0000-0000-0000-000000000003';

DELETE FROM auth_schema.role_permissions
WHERE role_id = 'a0000000-0000-0000-0000-000000000001'
  AND scope NOT IN (
      'server:create',
      'server:read',
      'server:update',
      'server:delete',
      'server:import',
      'server:export',
      'report:view',
      'report:send',
      'user:manage'
  );

DELETE FROM auth_schema.role_permissions
WHERE role_id = 'a0000000-0000-0000-0000-000000000002'
  AND scope NOT IN (
      'server:read',
      'server:update',
      'report:view'
  );

DELETE FROM auth_schema.role_permissions
WHERE role_id = 'a0000000-0000-0000-0000-000000000003'
  AND scope NOT IN (
      'server:read',
      'report:view'
  );

INSERT INTO auth_schema.role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'server:create'),
    ('a0000000-0000-0000-0000-000000000001', 'server:read'),
    ('a0000000-0000-0000-0000-000000000001', 'server:update'),
    ('a0000000-0000-0000-0000-000000000001', 'server:delete'),
    ('a0000000-0000-0000-0000-000000000001', 'server:import'),
    ('a0000000-0000-0000-0000-000000000001', 'server:export'),
    ('a0000000-0000-0000-0000-000000000001', 'report:view'),
    ('a0000000-0000-0000-0000-000000000001', 'report:send'),
    ('a0000000-0000-0000-0000-000000000001', 'user:manage'),
    ('a0000000-0000-0000-0000-000000000002', 'server:read'),
    ('a0000000-0000-0000-0000-000000000002', 'server:update'),
    ('a0000000-0000-0000-0000-000000000002', 'report:view'),
    ('a0000000-0000-0000-0000-000000000003', 'server:read'),
    ('a0000000-0000-0000-0000-000000000003', 'report:view')
ON CONFLICT (role_id, scope) DO NOTHING;
