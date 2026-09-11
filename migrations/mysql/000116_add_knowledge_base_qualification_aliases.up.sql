-- Idempotent column add for Oracle MySQL (ADD COLUMN IF NOT EXISTS is unsupported).
SET @db := DATABASE();
SET @exists := (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = @db
    AND TABLE_NAME = 'knowledge_bases'
    AND COLUMN_NAME = 'qualification_aliases'
);
SET @sql := IF(
  @exists = 0,
  'ALTER TABLE knowledge_bases ADD COLUMN qualification_aliases JSON NOT NULL DEFAULT (JSON_ARRAY())',
  'SELECT 1'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
