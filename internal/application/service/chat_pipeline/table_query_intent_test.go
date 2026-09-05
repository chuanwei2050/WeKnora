package chatpipeline

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestTableQueryIntentParsingPreservesUnknown(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, value := range []string{"true", "false", "null", `"false"`, "0", "missing"} {
			t.Run(fmt.Sprintf("strict=%v/value=%s", strict, value), func(t *testing.T) {
				fields := `"rewrite_query":"query","intent":"kb_search","complexity_level":"L1","reasoning_subtype":"explicit_fact","needs_entity_relation":false,"confidence":0.95`
				if value != "missing" {
					fields += `,"needs_table_query":` + value
				}
				cm := &types.ChatManage{}
				cm.ComplexityRouting.Enabled = strict
				(&PluginQueryUnderstand{}).parseOutput(cm, "{"+fields+"}")
				if value == "true" || value == "false" {
					if cm.NeedsTableQuery == nil || *cm.NeedsTableQuery != (value == "true") {
						t.Fatal("explicit classification lost")
					}
				} else if cm.NeedsTableQuery != nil {
					t.Fatal("invalid or absent classification became a skip decision")
				}
			})
		}
	}
	no := false
	cm := &types.ChatManage{PipelineState: types.PipelineState{NeedsTableQuery: &no}}
	(&PluginQueryUnderstand{}).parseOutput(cm, "not valid JSON")
	if cm.NeedsTableQuery != nil {
		t.Fatal("failed classification retained an obsolete skip decision")
	}
}

func TestNonTableIntentSkipsBeforeAnyDataAnalysisDependency(t *testing.T) {
	no := false
	evidence := &types.SearchResult{ID: "chunk", KnowledgeID: "doc", KnowledgeFilename: "data.xlsx", Content: "retrieved explanation"}
	cm := &types.ChatManage{PipelineState: types.PipelineState{NeedsTableQuery: &no, MergeResult: []*types.SearchResult{evidence}, SearchResult: []*types.SearchResult{evidence}}}
	called := false
	// Nil dependencies would panic if files, databases or models were consulted.
	if err := (&PluginDataAnalysis{}).OnEvent(context.Background(), types.DATA_ANALYSIS, cm, func() *PluginError { called = true; return nil }); err != nil || !called {
		t.Fatalf("non-table request did not continue: %v", err)
	}
	if len(cm.MergeResult) != 1 || cm.MergeResult[0] != evidence {
		t.Fatal("skipping analysis changed retrieval evidence")
	}
	cm.RerankOutcome = types.RerankOutcomeNoRelevantResult
	cm.MergeResult = nil
	if err := (&PluginDataAnalysis{}).OnEvent(context.Background(), types.DATA_ANALYSIS, cm, func() *PluginError { t.Fatal("empty retrieval should not be restored"); return nil }); err != ErrSearchNothing {
		t.Fatal("empty retrieval lost its outcome")
	}
}
