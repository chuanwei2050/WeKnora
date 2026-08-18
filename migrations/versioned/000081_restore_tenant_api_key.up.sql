-- Restore the tenant API key column required by the current application after
-- upgrading databases created by the former Plan3 migration line.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS api_key VARCHAR(256);
CREATE INDEX IF NOT EXISTS idx_tenants_api_key ON tenants(api_key);
