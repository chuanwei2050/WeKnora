WITH missing_untagged AS (
    SELECT kb.id AS knowledge_base_id, kb.tenant_id
    FROM knowledge_bases kb
    WHERE NOT EXISTS (
        SELECT 1
        FROM knowledge_tags tag
        WHERE tag.tenant_id = kb.tenant_id
          AND tag.knowledge_base_id = kb.id
          AND tag.name = '未分类'
    )
), sequence_base AS (
    SELECT COALESCE(MAX(seq_id), 0) AS max_seq_id
    FROM knowledge_tags
)
INSERT INTO knowledge_tags (
    id, tenant_id, knowledge_base_id, name, color, sort_order, seq_id, created_at, updated_at
)
SELECT
    lower(
        hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-' ||
        hex(randomblob(2)) || '-' || hex(randomblob(2)) || '-' || hex(randomblob(6))
    ),
    missing.tenant_id,
    missing.knowledge_base_id,
    '未分类',
    '',
    -1,
    sequence_base.max_seq_id + ROW_NUMBER() OVER (ORDER BY missing.knowledge_base_id),
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
FROM missing_untagged missing
CROSS JOIN sequence_base;

UPDATE knowledge_tags
SET sort_order = -1, updated_at = CURRENT_TIMESTAMP
WHERE name = '未分类';

UPDATE knowledges
SET tag_id = (
        SELECT tag.id
        FROM knowledge_tags tag
        WHERE tag.tenant_id = knowledges.tenant_id
          AND tag.knowledge_base_id = knowledges.knowledge_base_id
          AND tag.name = '未分类'
        LIMIT 1
    ),
    updated_at = CURRENT_TIMESTAMP
WHERE tag_id IS NULL OR tag_id = '';

UPDATE chunks
SET tag_id = (
        SELECT tag.id
        FROM knowledge_tags tag
        WHERE tag.tenant_id = chunks.tenant_id
          AND tag.knowledge_base_id = chunks.knowledge_base_id
          AND tag.name = '未分类'
        LIMIT 1
    ),
    updated_at = CURRENT_TIMESTAMP
WHERE tag_id IS NULL OR tag_id = '';
