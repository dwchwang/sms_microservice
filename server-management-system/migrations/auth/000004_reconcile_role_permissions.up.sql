-- Reconcile RBAC scopes to the agreed role matrix.
--
-- admin: full access, including monitor status.
-- operator: day-to-day operations without destructive delete or user management.
-- viewer: read-only plus server export for internal users.

UPDATE auth_schema.roles
SET description = 'Can operate servers, reports, and monitor status',
    updated_at = NOW()
WHERE id = 'a0000000-0000-0000-0000-000000000002';

UPDATE auth_schema.roles
SET description = 'Read-only access with export permission',
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
      'monitor:view',
      'report:view',
      'report:send',
      'user:manage'
  );

DELETE FROM auth_schema.role_permissions
WHERE role_id = 'a0000000-0000-0000-0000-000000000002'
  AND scope NOT IN (
      'server:create',
      'server:read',
      'server:update',
      'server:import',
      'server:export',
      'monitor:view',
      'report:view',
      'report:send'
  );

DELETE FROM auth_schema.role_permissions
WHERE role_id = 'a0000000-0000-0000-0000-000000000003'
  AND scope NOT IN (
      'server:read',
      'server:export',
      'report:view'
  );

INSERT INTO auth_schema.role_permissions (role_id, scope) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'monitor:view'),
    ('a0000000-0000-0000-0000-000000000002', 'server:create'),
    ('a0000000-0000-0000-0000-000000000002', 'server:import'),
    ('a0000000-0000-0000-0000-000000000002', 'server:export'),
    ('a0000000-0000-0000-0000-000000000002', 'monitor:view'),
    ('a0000000-0000-0000-0000-000000000002', 'report:send'),
    ('a0000000-0000-0000-0000-000000000003', 'server:export')
ON CONFLICT (role_id, scope) DO NOTHING;
