package sqlite

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestNormalizeDocumentUntaggedMigration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema := `
CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER);
CREATE TABLE knowledge_tags (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, name TEXT, color TEXT, sort_order INTEGER, seq_id INTEGER, created_at DATETIME, updated_at DATETIME);
CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, tag_id TEXT, updated_at DATETIME);
CREATE TABLE chunks (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, tag_id TEXT, updated_at DATETIME);
INSERT INTO knowledge_bases (id, tenant_id) VALUES ('kb', 1);
INSERT INTO knowledges (id, tenant_id, knowledge_base_id, tag_id) VALUES ('knowledge', 1, 'kb', '');
INSERT INTO chunks (id, tenant_id, knowledge_base_id, tag_id) VALUES ('chunk', 1, 'kb', NULL);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}

	migration, err := os.ReadFile("000026_normalize_document_untagged.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}

	var tagID string
	if err := db.QueryRow("SELECT id FROM knowledge_tags WHERE knowledge_base_id = 'kb' AND name = '未分类'").Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"knowledges", "chunks"} {
		var assigned string
		if err := db.QueryRow("SELECT tag_id FROM " + table + " LIMIT 1").Scan(&assigned); err != nil {
			t.Fatal(err)
		}
		if assigned != tagID {
			t.Fatalf("%s tag_id = %q, want %q", table, assigned, tagID)
		}
	}
}
