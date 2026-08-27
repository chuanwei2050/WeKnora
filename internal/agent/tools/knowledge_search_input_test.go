package tools

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidateKnowledgeSearchQueriesRejectsResourceAmplification(t *testing.T) {
	tests := []struct {
		name    string
		queries []string
	}{
		{name: "too many", queries: []string{"1", "2", "3", "4", "5", "6"}},
		{name: "single too long", queries: []string{strings.Repeat("a", maxKnowledgeSearchQueryBytes+1)}},
		{name: "total too long", queries: []string{strings.Repeat("a", 3000), strings.Repeat("b", 3000), strings.Repeat("c", 3000)}},
		{name: "blank", queries: []string{"  "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateKnowledgeSearchQueries(test.queries); err == nil {
				t.Fatal("expected query boundary validation error")
			}
		})
	}
}

func TestAgentCandidateLimitPreservesExplicitScopes(t *testing.T) {
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledge, KnowledgeIDs: []string{"a", "b"}}}
	results := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "a-high", KnowledgeID: "a", Score: 0.9}},
		{SearchResult: &types.SearchResult{ID: "a-low", KnowledgeID: "a", Score: 0.8}},
		{SearchResult: &types.SearchResult{ID: "b", KnowledgeID: "b", Score: 0.1}},
	}
	got := limitAgentCandidates(results, 2, targets)
	if len(got) != 2 || got[0].KnowledgeID != "a" || got[1].KnowledgeID != "b" {
		t.Fatalf("explicit scopes were not preserved: %+v", got)
	}
}

func TestAgentDedupKeepsSameContentAcrossExplicitFiles(t *testing.T) {
	tool := &KnowledgeSearchTool{}
	results := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "a", KnowledgeID: "a", Content: "same", Score: 0.9}},
		{SearchResult: &types.SearchResult{ID: "b", KnowledgeID: "b", Content: "same", Score: 0.8}},
	}
	if got := tool.deduplicateResults(results); len(got) != 2 {
		t.Fatalf("cross-file candidates were deduplicated: %+v", got)
	}
}

func TestValidateKnowledgeSearchQueriesAcceptsBoundedInput(t *testing.T) {
	if err := validateKnowledgeSearchQueries([]string{"RAG 如何工作？", "有哪些限制？"}); err != nil {
		t.Fatalf("bounded queries were rejected: %v", err)
	}
}

func TestValidateKnowledgeSearchInputRejectsKnowledgeBaseAmplification(t *testing.T) {
	tests := []KnowledgeSearchInput{
		{Queries: []string{"q"}, KnowledgeBaseIDs: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}},
		{Queries: []string{"q"}, KnowledgeBaseIDs: []string{strings.Repeat("a", maxKnowledgeSearchKBIDBytes+1)}},
		{Queries: []string{"q"}, KnowledgeBaseIDs: []string{strings.Repeat("a", 210), strings.Repeat("b", 210), strings.Repeat("c", 210), strings.Repeat("d", 210), strings.Repeat("e", 210), strings.Repeat("f", 210), strings.Repeat("g", 210), strings.Repeat("h", 210), strings.Repeat("i", 210), strings.Repeat("j", 210)}},
	}
	for i, input := range tests {
		if err := validateKnowledgeSearchInput(input); err == nil {
			t.Fatalf("case %d: expected knowledge base boundary validation error", i)
		}
	}
}
