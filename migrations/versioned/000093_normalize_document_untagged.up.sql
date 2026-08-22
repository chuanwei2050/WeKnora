-- Normalize document knowledge without a tag into the real "未分类" tag.
DO $$
DECLARE
    kb_record RECORD;
    untagged_tag_id VARCHAR(36);
BEGIN
    FOR kb_record IN
        SELECT kb.id, kb.tenant_id
        FROM knowledge_bases kb
    LOOP
        SELECT id INTO untagged_tag_id
        FROM knowledge_tags
        WHERE tenant_id = kb_record.tenant_id
          AND knowledge_base_id = kb_record.id
          AND name = '未分类'
        LIMIT 1;

        IF untagged_tag_id IS NULL THEN
            untagged_tag_id := gen_random_uuid()::VARCHAR(36);
            INSERT INTO knowledge_tags (
                id, tenant_id, knowledge_base_id, name, color, sort_order, created_at, updated_at
            ) VALUES (
                untagged_tag_id, kb_record.tenant_id, kb_record.id, '未分类', '', -1, NOW(), NOW()
            );
        ELSE
            UPDATE knowledge_tags
            SET sort_order = -1, updated_at = NOW()
            WHERE id = untagged_tag_id;
        END IF;

        UPDATE knowledges
        SET tag_id = untagged_tag_id, updated_at = NOW()
        WHERE tenant_id = kb_record.tenant_id
          AND knowledge_base_id = kb_record.id
          AND (tag_id IS NULL OR tag_id = '');

        UPDATE chunks
        SET tag_id = untagged_tag_id, updated_at = NOW()
        WHERE tenant_id = kb_record.tenant_id
          AND knowledge_base_id = kb_record.id
          AND (tag_id IS NULL OR tag_id = '');

        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'embeddings' AND column_name = 'tag_id'
        ) THEN
            UPDATE embeddings
            SET tag_id = untagged_tag_id
            WHERE knowledge_base_id = kb_record.id
              AND (tag_id IS NULL OR tag_id = '');
        END IF;
    END LOOP;
END $$;
