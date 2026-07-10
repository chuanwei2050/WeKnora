-- Description: Remove BidReview role and knowledge base creator permission columns.

DROP INDEX IF EXISTS idx_knowledge_bases_tenant_created_by;

ALTER TABLE knowledge_bases
    DROP COLUMN IF EXISTS created_by;

ALTER TABLE users
    DROP COLUMN IF EXISTS bidreview_role;
