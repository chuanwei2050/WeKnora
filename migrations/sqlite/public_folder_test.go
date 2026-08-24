package sqlite

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTagPublicFolderMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:public-folder-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, name TEXT NOT NULL);`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_tags (id, name) VALUES ('existing', '公共文件');`).Error)

	up, err := os.ReadFile("000034_knowledge_tag_public_folder.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)

	var row struct {
		Name     string
		IsPublic int `gorm:"column:is_public"`
	}
	require.NoError(t, db.Raw(`SELECT name, is_public FROM knowledge_tags WHERE id = 'existing'`).Scan(&row).Error)
	require.Equal(t, "公共文件", row.Name)
	require.Zero(t, row.IsPublic)

	down, err := os.ReadFile("000034_knowledge_tag_public_folder.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(down)).Error)
}
