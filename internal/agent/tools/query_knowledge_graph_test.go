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
