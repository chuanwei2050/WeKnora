package chatpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

// --- IntoChatMessage tests ---

func TestIntoChatMessage_NoKBRetrieval(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "hello world",
		},
		PipelineState: types.PipelineState{
			Intent: types.IntentChitchat,
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("next() was not called")
	}
	if cm.UserContent != "hello world" {
		t.Errorf("UserContent: got %q, want %q", cm.UserContent, "hello world")
	}
}

func TestIntoChatMessage_WithMergeResults(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "test query",
			SummaryConfig: types.SummaryConfig{
				ContextTemplate: "Question: {{query}}\n\nReferences:\n{{contexts}}",
			},
		},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{
				{Content: "chunk A content"},
				{Content: "chunk B content"},
			},
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	nextCalled := false
	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		nextCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Fatal("next() was not called")
	}
	if cm.UserContent == "" {
		t.Fatal("expected UserContent to be populated")
	}
	if !contains(cm.UserContent, "test query") {
		t.Errorf("UserContent should contain query, got: %s", cm.UserContent)
	}
	if !contains(cm.UserContent, "chunk A content") {
		t.Errorf("UserContent should contain chunk A, got: %s", cm.UserContent)
	}
}

func TestStructuredAnswerOutputRulesAreAppendedWithoutChangingEvidence(t *testing.T) {
	result := &types.SearchResult{
		ID:        "analysis",
		MatchType: types.MatchTypeDataAnalysis,
		Content:   `Executed SQL: SELECT master_count FROM k_secret {"master_count":"41"}`,
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:         "有多少人",
			SummaryConfig: types.SummaryConfig{ContextTemplate: "{{contexts}}\n{{query}}"},
		},
		PipelineState: types.PipelineState{
			MergeResult: []*types.SearchResult{result},
		},
	}
	plugin := &PluginIntoChatMessage{}
	if err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, manage, func() *PluginError { return nil }); err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if !strings.Contains(manage.UserContent, result.Content) {
		t.Fatal("structured evidence was changed before answer generation")
	}
	for _, rule := range []string{
		"SQL", "物理表名", "内部标识", "字段别名", "原始载荷", "自然语言",
		"统计、聚合、排序和计算类问题以 SQL 结果为准",
		"普通事实和列举类问题不得因 SQL 未命中而忽略其他证据",
		"实际表述", "不得臆造同义", "无关相邻记录",
		"不能单独证明完整总数或全集",
	} {
		if !strings.Contains(manage.UserContent, rule) {
			t.Fatalf("missing output rule %q", rule)
		}
	}
}

func TestStructuredEvidenceIsInjectedWhenTemplateOmitsContexts(t *testing.T) {
	result := &types.SearchResult{ID: "analysis", MatchType: types.MatchTypeDataAnalysis, Content: `{"rows":[[41]]}`}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "有多少人", SummaryConfig: types.SummaryConfig{ContextTemplate: "{{query}}"}},
		PipelineState:   types.PipelineState{MergeResult: []*types.SearchResult{result}},
	}

	if err := (&PluginIntoChatMessage{}).OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manage.UserContent, "<structured_query_evidence>") || !strings.Contains(manage.UserContent, result.Content) {
		t.Fatalf("structured evidence missing from prompt: %s", manage.UserContent)
	}
}

func TestStructuredAndRerankedEvidenceAreBothRenderedForAnswerReview(t *testing.T) {
	reranked := &types.SearchResult{ID: "reranked", Content: "姓名：夏雨欣；证书：系统集成项目管理师"}
	structured := &types.SearchResult{ID: "structured", MatchType: types.MatchTypeDataAnalysis, Content: `{"rows":[["许乃汉"]]}`}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:         "列出持有系统集成项目管理工程师证书的人员",
			SummaryConfig: types.SummaryConfig{ContextTemplate: "{{contexts}}\n{{query}}"},
		},
		PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{reranked, structured}},
	}

	if err := (&PluginIntoChatMessage{}).OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, manage, func() *PluginError { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []string{
		reranked.Content, structured.Content,
		"以 SQL 结果为准", "不得因 SQL 未命中而忽略其他证据", "实际表述", "无关相邻记录",
	} {
		if !strings.Contains(manage.UserContent, evidence) {
			t.Fatalf("answer prompt missing %q: %s", evidence, manage.UserContent)
		}
	}
}

func TestIntoChatMessage_ImageDescriptionAppended(t *testing.T) {
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                   "what is this?",
			ChatModelSupportsVision: false,
		},
		PipelineState: types.PipelineState{
			Intent:           types.IntentChitchat,
			ImageDescription: "a cat sitting on a mat",
		},
	}
	plugin := &PluginIntoChatMessage{messageService: nil}
	_ = plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, cm, func() *PluginError {
		return nil
	})
	if !contains(cm.UserContent, "a cat sitting on a mat") {
		t.Errorf("UserContent should contain image description, got: %s", cm.UserContent)
	}
}

// --- PipelineBuilder tests ---

func TestPipelineBuilder_Basic(t *testing.T) {
	pipeline := types.NewPipelineBuilder().
		Add(types.LOAD_HISTORY).
		Add(types.CHAT_COMPLETION_STREAM).
		Build()

	if len(pipeline) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(pipeline))
	}
	if pipeline[0] != types.LOAD_HISTORY {
		t.Errorf("stage 0: got %v, want %v", pipeline[0], types.LOAD_HISTORY)
	}
}

func TestPipelineBuilder_AddIf(t *testing.T) {
	pipeline := types.NewPipelineBuilder().
		Add(types.LOAD_HISTORY).
		AddIf(false, types.QUERY_UNDERSTAND).
		AddIf(true, types.CHAT_COMPLETION_STREAM).
		Build()

	if len(pipeline) != 2 {
		t.Fatalf("expected 2 stages (QUERY_UNDERSTAND skipped), got %d", len(pipeline))
	}
	if pipeline[1] != types.CHAT_COMPLETION_STREAM {
		t.Errorf("stage 1: got %v, want %v", pipeline[1], types.CHAT_COMPLETION_STREAM)
	}
}

func TestPipelineBuilder_Empty(t *testing.T) {
	pipeline := types.NewPipelineBuilder().Build()
	if len(pipeline) != 0 {
		t.Fatalf("expected 0 stages, got %d", len(pipeline))
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
