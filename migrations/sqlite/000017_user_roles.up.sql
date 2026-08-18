ALTER TABLE users ADD COLUMN role VARCHAR(32) NOT NULL DEFAULT 'member';

UPDATE users
SET role = CASE
    WHEN can_access_all_tenants = 1 OR bidreview_role = 'platform_admin' THEN 'platform_admin'
    WHEN bidreview_role = 'tenant_admin' OR COALESCE(bidreview_role, '') = '' THEN 'tenant_admin'
    ELSE 'member'
END;

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
