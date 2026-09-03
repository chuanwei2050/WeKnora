package sqlite

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDocumentDirectoryCategoryScopeMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:directory-category-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, db.Exec(`
CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL);
CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL, name TEXT NOT NULL, color TEXT, sort_order INTEGER, seq_id INTEGER, created_at DATETIME, updated_at DATETIME);
CREATE TABLE knowledge_directories (
  id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL, parent_id TEXT,
  parent_key TEXT NOT NULL DEFAULT '', name TEXT NOT NULL, normalized_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active', deletion_task_id TEXT, created_at DATETIME, updated_at DATETIME,
  UNIQUE (tenant_id, knowledge_base_id, id),
  FOREIGN KEY (tenant_id, knowledge_base_id, parent_id) REFERENCES knowledge_directories(tenant_id, knowledge_base_id, id),
  UNIQUE (tenant_id, knowledge_base_id, parent_key, normalized_name)
);
CREATE INDEX idx_knowledge_directories_parent ON knowledge_directories(tenant_id, knowledge_base_id, parent_key, status);
CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL, tag_id TEXT NOT NULL, directory_id TEXT REFERENCES knowledge_directories(id));
CREATE INDEX idx_knowledges_directory ON knowledges(tenant_id, knowledge_base_id, directory_id);
INSERT INTO knowledge_bases VALUES ('kb', 1);
INSERT INTO knowledge_tags VALUES ('category-a', 1, 'kb', '分类 A', '', 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO knowledge_directories (id, tenant_id, knowledge_base_id, name, normalized_name) VALUES ('directory', 1, 'kb', '目录', '目录');
INSERT INTO knowledges VALUES ('knowledge', 1, 'kb', 'category-a', 'directory');
`).Error)

	migration, err := os.ReadFile("000040_scope_document_directories_by_tag.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(migration)).Error)

	var untaggedID string
	require.NoError(t, db.Raw("SELECT id FROM knowledge_tags WHERE knowledge_base_id = 'kb' AND name = '未分类'").Scan(&untaggedID).Error)
	var directoryTagID, knowledgeTagID string
	require.NoError(t, db.Raw("SELECT tag_id FROM knowledge_directories WHERE id = 'directory'").Scan(&directoryTagID).Error)
	require.NoError(t, db.Raw("SELECT tag_id FROM knowledges WHERE id = 'knowledge'").Scan(&knowledgeTagID).Error)
	require.Equal(t, untaggedID, directoryTagID)
	require.Equal(t, untaggedID, knowledgeTagID)
	require.NoError(t, db.Exec("INSERT INTO knowledge_directories (id, tenant_id, knowledge_base_id, tag_id, name, normalized_name) VALUES ('same-a', 1, 'kb', 'category-a', '同名', '同名')").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_directories (id, tenant_id, knowledge_base_id, tag_id, name, normalized_name) VALUES ('same-b', 1, 'kb', ?, '同名', '同名')", untaggedID).Error)
	require.Error(t, db.Exec("INSERT INTO knowledge_directories (id, tenant_id, knowledge_base_id, tag_id, parent_id, parent_key, name, normalized_name) VALUES ('cross-child', 1, 'kb', ?, 'same-a', 'same-a', '子目录', '子目录')", untaggedID).Error)
}
