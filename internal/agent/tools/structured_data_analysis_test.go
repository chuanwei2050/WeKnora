package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/structuredquery"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestStructuredDataAnalysisToolDelegatesNaturalLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request structuredquery.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Question != "有多少人" || len(request.DatasetIDs) != 1 || request.DatasetIDs[0] != "dataset-1" {
			t.Fatalf("unexpected request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"route":"sql","sql":"SELECT count(*) FROM people","columns":["count"],"rows":[[42]]}`))
	}))
	defer server.Close()
	knowledge := &types.Knowledge{ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1", EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted, Metadata: types.JSON(`{"structured_dataset_id":"dataset-1"}`)}
	tool := &StructuredDataAnalysisTool{
		knowledgeService: sqlRegressionKnowledgeService{knowledge: knowledge},
		searchTargets:    types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledge, KnowledgeBaseID: "kb-1", TenantID: 7, KnowledgeIDs: []string{"knowledge-1"}}},
		client:           structuredquery.Client{BaseURL: server.URL, APIKey: "key"},
	}
	args, _ := json.Marshal(StructuredDataAnalysisInput{KnowledgeID: "knowledge-1", Question: "有多少人"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil || !result.Success || result.Data["query"] == "" {
		t.Fatalf("unexpected result: result=%#v err=%v", result, err)
	}
}
