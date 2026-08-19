-- Description: Persist BidReview roles and knowledge base creators for owner-scoped KB management.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bidreview_role VARCHAR(32) NOT NULL DEFAULT 'member';

ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS created_by VARCHAR(36);

CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_created_by
    ON knowledge_bases(tenant_id, created_by);

COMMENT ON COLUMN users.bidreview_role IS
    'BidReview SSO role used by embedded permission gates: platform_admin, tenant_admin, or member';

COMMENT ON COLUMN knowledge_bases.created_by IS
    'User ID that created this knowledge base; NULL historical rows are admin-managed only';
