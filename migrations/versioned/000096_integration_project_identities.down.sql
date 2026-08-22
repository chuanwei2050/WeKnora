DROP INDEX IF EXISTS idx_integration_external_identities_client_active;

ALTER TABLE integration_external_identities
    DROP CONSTRAINT uq_integration_external_identity;

ALTER TABLE integration_external_identities
    ADD CONSTRAINT uq_integration_external_identity
    UNIQUE(identity_provider_id, external_tenant_id, external_user_id);

ALTER TABLE integration_external_identities
    DROP COLUMN active,
    DROP COLUMN client_id;

ALTER TABLE integration_clients
    DROP COLUMN administrator_user_id;
