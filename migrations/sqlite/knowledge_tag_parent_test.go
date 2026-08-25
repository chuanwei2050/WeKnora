package sqlite

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTagParentMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge-tag-parent-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_tags (
		id TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		knowledge_base_id TEXT NOT NULL,
		name TEXT NOT NULL
	);`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name)
		VALUES ('existing', 1, 'kb', '现有文件夹');`).Error)

	up, err := os.ReadFile("000036_knowledge_tag_parent.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)

	var parentID *string
	require.NoError(t, db.Raw(`SELECT parent_id FROM knowledge_tags WHERE id = 'existing'`).Scan(&parentID).Error)
	require.Nil(t, parentID)

	down, err := os.ReadFile("000036_knowledge_tag_parent.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
}

func TestKnowledgeTagParentMigrationRejectsUnsafeRollback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge-tag-parent-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_tags (
		id TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		knowledge_base_id TEXT NOT NULL,
		name TEXT NOT NULL
	);`).Error)

	up, err := os.ReadFile("000036_knowledge_tag_parent.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, parent_id)
		VALUES ('child', 1, 'kb', '二级文件夹', 'parent');`).Error)

	down, err := os.ReadFile("000036_knowledge_tag_parent.down.sql")
	require.NoError(t, err)
	require.Error(t, db.Exec(string(down)).Error)
}

func TestKnowledgeTagParentIntegrityMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:knowledge-tag-parent-integrity?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_tags (
		id TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		knowledge_base_id TEXT NOT NULL,
		name TEXT NOT NULL
	);`).Error)

	for _, name := range []string{"000036_knowledge_tag_parent.up.sql", "000037_knowledge_tag_parent_integrity.up.sql"} {
		migration, readErr := os.ReadFile(name)
		require.NoError(t, readErr)
		require.NoError(t, db.Exec(string(migration)).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name)
		VALUES ('parent', 1, 'kb', '一级文件夹');`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, parent_id)
		VALUES ('child', 1, 'kb', '二级文件夹', 'parent');`).Error)
	require.Error(t, db.Exec(`DELETE FROM knowledge_tags WHERE id = 'parent'`).Error)
	require.Error(t, db.Exec(`INSERT INTO knowledge_tags (id, tenant_id, knowledge_base_id, name, parent_id)
		VALUES ('orphan', 1, 'kb', '孤儿文件夹', 'missing');`).Error)
	require.NoError(t, db.Exec(`DELETE FROM knowledge_tags WHERE id = 'child'`).Error)
	require.NoError(t, db.Exec(`DELETE FROM knowledge_tags WHERE id = 'parent'`).Error)
}
