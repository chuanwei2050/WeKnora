package sqlite

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAllowDuplicateKnowledgeTagNamesMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:duplicate-tag-name-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
CREATE TABLE knowledge_tags (
  id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL, name TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_knowledge_tags_kb_name ON knowledge_tags(tenant_id, knowledge_base_id, name);
INSERT INTO knowledge_tags VALUES ('untagged', 1, 'kb', '未分类');
INSERT INTO knowledge_tags VALUES ('contract-a', 1, 'kb', '合同');
`).Error)

	migration, err := os.ReadFile("000041_allow_duplicate_knowledge_tag_names.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_tags VALUES ('contract-b', 1, 'kb', '合同')").Error)
	require.Error(t, db.Exec("INSERT INTO knowledge_tags VALUES ('untagged-b', 1, 'kb', '未分类')").Error)
}

func TestAddDataSourceAutoTagIDMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:data-source-auto-tag-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE data_sources (id TEXT PRIMARY KEY)").Error)

	migration, err := os.ReadFile("000042_add_data_source_auto_tag_id.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)
	require.NoError(t, db.Exec("INSERT INTO data_sources (id, auto_tag_id) VALUES ('source', 'folder')").Error)

	var autoTagID string
	require.NoError(t, db.Raw("SELECT auto_tag_id FROM data_sources WHERE id = 'source'").Scan(&autoTagID).Error)
	require.Equal(t, "folder", autoTagID)
}
