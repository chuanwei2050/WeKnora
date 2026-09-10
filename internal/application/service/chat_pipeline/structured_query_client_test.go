package chatpipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type structuredKnowledgeFixture struct{ interfaces.KnowledgeService }

func (structuredKnowledgeFixture) ListKnowledgeByKnowledgeBaseID(context.Context, string) ([]*types.Knowledge, error) {
	return []*types.Knowledge{{
		ID: "knowledge-1", EnableStatus: "enabled", ParseStatus: types.ParseStatusCompleted,
		Metadata: types.JSON(`{"structured_dataset_id":"dataset-1"}`),
	}}, nil
}

func TestStructuredQueryStartsBeforeRetrievalAndMergesLater(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if ids, exists := payload["dataset_ids"].([]any); !exists || len(ids) != 1 || ids[0] != "dataset-1" {
			t.Fatalf("request must carry the authorized dataset ids: %#v", payload)
		}
		if r.Header.Get("X-Tenant-ID") != "7" {
			t.Fatalf("tenant header missing: %q", r.Header.Get("X-Tenant-ID"))
		}
		called <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"route":"sql","sql":"SELECT COUNT(*) FROM people","columns":["count"],"rows":[[41]],"model_calls":1,"timings":{"total_ms":12},"sources":[]}`))
	}))
	defer server.Close()
	plugin := &PluginDataAnalysis{
		config:           &config.Config{StructuredQuery: &config.StructuredQueryConfig{Enabled: true, BaseURL: server.URL, APIKey: "key", RequestTimeout: 2}},
		knowledgeService: structuredKnowledgeFixture{},
	}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		Query: "硕士人数", TenantID: 7,
		SearchTargets: types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 7}},
	}}
	nextCalled := false
	if err := plugin.OnEvent(context.Background(), types.STRUCTURED_QUERY_START, manage, func() *PluginError { nextCalled = true; return nil }); err != nil || !nextCalled {
		t.Fatalf("retrieval was blocked: %v", err)
	}
	<-called
	if err := plugin.OnEvent(context.Background(), types.DATA_ANALYSIS, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(manage.DataAnalysisResult) != 1 || manage.DataAnalysisResult[0].MatchType != types.MatchTypeDataAnalysis {
		t.Fatalf("structured result not merged: %#v", manage.DataAnalysisResult)
	}
}

func TestSuccessfulStructuredQueryKeepsRerankedEvidenceForCrossCheck(t *testing.T) {
	done := make(chan []*types.SearchResult, 1)
	structured := &types.SearchResult{ID: "structured", MatchType: types.MatchTypeDataAnalysis}
	done <- []*types.SearchResult{structured}
	close(done)
	manage := &types.ChatManage{
		PipelineState: types.PipelineState{
			StructuredQueryDone: done,
			MergeResult:         []*types.SearchResult{{ID: "partial-passage"}},
		},
	}
	plugin := &PluginDataAnalysis{config: &config.Config{StructuredQuery: &config.StructuredQueryConfig{Enabled: true, BaseURL: "http://unused"}}}

	if err := plugin.OnEvent(context.Background(), types.DATA_ANALYSIS, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(manage.MergeResult) != 2 || manage.MergeResult[0].ID != "partial-passage" || manage.MergeResult[1] != structured {
		t.Fatalf("structured and reranked evidence were not both retained: %#v", manage.MergeResult)
	}
}

func TestSuccessfulStructuredQueryEmitsTableAnalysisStage(t *testing.T) {
	bus := &stageEventBus{handlers: make(map[types.EventType][]types.EventHandler)}
	var call event.AgentToolCallData
	var result event.AgentToolResultData
	bus.On(types.EventType(event.EventAgentToolCall), func(_ context.Context, evt types.Event) error {
		call = evt.Data.(event.AgentToolCallData)
		return nil
	})
	bus.On(types.EventType(event.EventAgentToolResult), func(_ context.Context, evt types.Event) error {
		result = evt.Data.(event.AgentToolResultData)
		return nil
	})
	done := make(chan []*types.SearchResult, 1)
	done <- []*types.SearchResult{{ID: "structured", MatchType: types.MatchTypeDataAnalysis}}
	close(done)
	manage := &types.ChatManage{
		PipelineState:   types.PipelineState{StructuredQueryDone: done},
		PipelineContext: types.PipelineContext{EventBus: bus},
	}
	plugin := &PluginDataAnalysis{config: &config.Config{StructuredQuery: &config.StructuredQueryConfig{Enabled: true, BaseURL: "http://unused"}}}

	if err := plugin.OnEvent(context.Background(), types.DATA_ANALYSIS, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if call.ToolName != "data_analysis" || call.Hint != "表格分析" {
		t.Fatalf("unexpected table-analysis start event: %#v", call)
	}
	if result.ToolName != "data_analysis" || !result.Success || result.Data["status"] != "completed" {
		t.Fatalf("unexpected table-analysis result event: %#v", result)
	}
}
