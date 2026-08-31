package types

import "testing"

func TestKnowledgeIDsForKBPreservesExplicitEmptyScope(t *testing.T) {
	ids := SearchTargets{{
		Type:            SearchTargetTypeKnowledge,
		KnowledgeBaseID: "kb-1",
		TenantID:        1,
		KnowledgeIDs:    []string{},
	}}.KnowledgeIDsForKB("kb-1")
	if ids == nil || len(ids) != 0 {
		t.Fatalf("explicit empty knowledge scope = %#v, want non-nil empty slice", ids)
	}
	if ids := (SearchTargets{{Type: SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 1}}).KnowledgeIDsForKB("kb-1"); ids != nil {
		t.Fatalf("whole-KB scope = %#v, want nil", ids)
	}
}
