package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestQueryKnowledgeGraphToolRejectsWhenRelationTraversalIsNotNeeded(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.GraphQueryAllowedContextKey, false)
	args, err := json.Marshal(QueryKnowledgeGraphInput{
		KnowledgeBaseIDs: []string{"kb-1"},
		Query:            "产品价格是多少",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := (&QueryKnowledgeGraphTool{}).Execute(ctx, args)
	if err == nil || result == nil || result.Success {
		t.Fatalf("expected graph query to be rejected, result=%+v err=%v", result, err)
	}
}

func TestConfiguredGraphRelationTypesUseFormalSchemaTags(t *testing.T) {
	kb := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{GraphEnabled: true},
		ExtractConfig: &types.ExtractConfig{
			Tags:      []string{"uses", "tests"},
			Relations: []*types.GraphRelation{{Type: "few-shot-only"}},
		},
	}

	got := configuredGraphRelationTypes(kb)
	if len(got) != 2 || got[0] != "uses" || got[1] != "tests" {
		t.Fatalf("expected formal schema tags, got %v", got)
	}
	if got := configuredGraphRelationTypes(&types.KnowledgeBase{IndexingStrategy: types.IndexingStrategy{GraphEnabled: true}}); got != nil {
		t.Fatalf("expected nil without extract config, got %v", got)
	}
}
