ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(32) NOT NULL DEFAULT 'member';

UPDATE users
SET role = CASE
    WHEN can_access_all_tenants = TRUE OR bidreview_role = 'platform_admin' THEN 'platform_admin'
    WHEN bidreview_role = 'tenant_admin' OR COALESCE(bidreview_role, '') = '' THEN 'tenant_admin'
    ELSE 'member'
END;

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

ALTER TABLE users
    ADD CONSTRAINT chk_users_role
    CHECK (role IN ('platform_admin', 'tenant_admin', 'member'));
