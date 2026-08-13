package neo4j

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestRemoveHyphen(t *testing.T) {
	t.Parallel()
	if got := _remove_hyphen("kb-id"); got != "kb_id" {
		t.Fatalf("_remove_hyphen() = %q, want kb_id", got)
	}
}

func TestListI2ListS(t *testing.T) {
	t.Parallel()
	got := listI2listS([]any{"a", 2, true})
	want := []string{"a", "2", "true"}
	if len(got) != len(want) {
		t.Fatalf("listI2listS() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("listI2listS()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNeo4jRepositoryLabelsWithoutDriver(t *testing.T) {
	t.Parallel()
	repo := &Neo4jRepository{nodePrefix: "ENTITY"}
	namespace := types.NameSpace{KnowledgeBase: "kb-1", Knowledge: "doc-2"}
	got := repo.Labels(namespace)
	want := []string{"ENTITYkb_1", "ENTITYdoc_2"}
	if len(got) != len(want) {
		t.Fatalf("Labels() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Labels()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if label := repo.Label(namespace); label != "ENTITYkb_1:ENTITYdoc_2" {
		t.Fatalf("Label() = %q", label)
	}
}
