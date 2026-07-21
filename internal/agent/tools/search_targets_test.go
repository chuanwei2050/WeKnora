package tools

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestResolveSearchTargetsUsesAllTargetsWhenFilterOmitted(t *testing.T) {
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 10020},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-2", TenantID: 10020},
	}

	resolved, usedSingleTargetFallback := resolveSearchTargets(targets, nil)

	if len(resolved) != 2 || usedSingleTargetFallback {
		t.Fatalf("resolveSearchTargets() = (%v, %v), want both targets without fallback", resolved, usedSingleTargetFallback)
	}
}

func TestResolveSearchTargetsKeepsMatchingFilter(t *testing.T) {
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 10020},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-2", TenantID: 10020},
	}

	resolved, usedSingleTargetFallback := resolveSearchTargets(targets, []string{"kb-2"})

	if len(resolved) != 1 || resolved[0].KnowledgeBaseID != "kb-2" || usedSingleTargetFallback {
		t.Fatalf("resolveSearchTargets() = (%v, %v), want kb-2 without fallback", resolved, usedSingleTargetFallback)
	}
}

func TestResolveSearchTargetsFallsBackForSingleAuthorizedTarget(t *testing.T) {
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "correct-kb", TenantID: 10020},
	}

	resolved, usedSingleTargetFallback := resolveSearchTargets(targets, []string{"mistyped-kb"})

	if len(resolved) != 1 || resolved[0].KnowledgeBaseID != "correct-kb" || !usedSingleTargetFallback {
		t.Fatalf("resolveSearchTargets() = (%v, %v), want authorized single target fallback", resolved, usedSingleTargetFallback)
	}
}

func TestResolveSearchTargetsDoesNotExpandInvalidMultiTargetFilter(t *testing.T) {
	targets := types.SearchTargets{
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 10020},
		{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-2", TenantID: 10020},
	}

	resolved, usedSingleTargetFallback := resolveSearchTargets(targets, []string{"mistyped-kb"})

	if len(resolved) != 0 || usedSingleTargetFallback {
		t.Fatalf("resolveSearchTargets() = (%v, %v), want empty result without scope expansion", resolved, usedSingleTargetFallback)
	}
}
