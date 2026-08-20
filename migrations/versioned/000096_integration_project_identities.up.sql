ALTER TABLE integration_clients
    ADD COLUMN administrator_user_id VARCHAR(36) NOT NULL DEFAULT '';

ALTER TABLE integration_external_identities
    ADD COLUMN client_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE integration_external_identities
    DROP CONSTRAINT uq_integration_external_identity;

ALTER TABLE integration_external_identities
    ADD CONSTRAINT uq_integration_external_identity
    UNIQUE(client_id, external_tenant_id, external_user_id);

CREATE INDEX idx_integration_external_identities_client_active
    ON integration_external_identities(client_id, active);
