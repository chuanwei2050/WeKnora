package versioned

import (
	"os"
	"strings"
	"testing"
)

func TestNormalizeDocumentUntaggedMigration(t *testing.T) {
	up, err := os.ReadFile("000093_normalize_document_untagged.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	upSQL := string(up)
	for _, required := range []string{
		"INSERT INTO knowledge_tags",
		"UPDATE knowledges",
		"UPDATE chunks",
		"UPDATE embeddings",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("up migration missing %q", required)
		}
	}

	down, err := os.ReadFile("000093_normalize_document_untagged.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(down)
	if strings.Contains(downSQL, "UPDATE knowledges") || strings.Contains(downSQL, "DELETE FROM knowledge_tags") {
		t.Fatal("down migration must preserve normalized folder assignments")
	}
}
