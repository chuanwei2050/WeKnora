-- Data normalization is intentionally retained on rollback. Clearing tags here
-- would also erase folder assignments made by users after the migration.
DO $$ BEGIN
    RAISE NOTICE '[Migration 000093 Rollback] Preserved normalized 未分类 assignments';
END $$;
