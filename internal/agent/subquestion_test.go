package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type recordingSearchTool struct {
	mu    sync.Mutex
	args  []string
	byArg map[string]string
}

func (t *recordingSearchTool) Name() string        { return agenttools.ToolKnowledgeSearch }
func (t *recordingSearchTool) Description() string { return "test search" }
func (t *recordingSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"queries":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":5}},"required":["queries"],"additionalProperties":false}`)
}
func (t *recordingSearchTool) Execute(_ context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	query := input.Queries[0]
	t.mu.Lock()
	t.args = append(t.args, query)
	t.mu.Unlock()
	output, ok := t.byArg[query]
	if !ok {
		return &types.ToolResult{Success: false, Error: "not found"}, nil
	}
	return &types.ToolResult{Success: true, Output: output}, nil
}

func newSubQuestionTestEngine(t *testing.T, tool types.Tool) *AgentEngine {
	t.Helper()
	registry := agenttools.NewToolRegistry()
	registry.RegisterTool(tool)
	engine := NewAgentEngine(&types.AgentConfig{}, &mockChat{}, registry, event.NewEventBus(), nil, nil, nil, "session", "")
	require.NotNil(t, engine)
	return engine
}

func TestExecuteSubQuestionPlanRunsOrderedAndPassesConfirmedDependencies(t *testing.T) {
	first := "原问题：比较 A 和 B\n当前子问题：确认 A 的适用条件"
	search := &recordingSearchTool{byArg: map[string]string{first: "A 的适用条件已确认"}}
	engine := newSubQuestionTestEngine(t, search)
	engine.config.SubQuestionPlan = &types.SubQuestionPlan{
		Questions: []types.SubQuestion{
			{Index: 1, Query: "确认 A 的适用条件", Required: true},
			{Index: 2, Query: "基于前序材料比较 B", DependsOn: []int{1}, Required: true},
		},
		MaxQuestions: 4, MaxModelCalls: 3, MaxDurationMs: 30000,
	}

	state := &types.AgentState{}
	messages := engine.executeSubQuestionPlan(context.Background(), "比较 A 和 B", []chat.Message{{Role: "user", Content: "比较 A 和 B"}}, state)

	search.mu.Lock()
	args := append([]string(nil), search.args...)
	search.mu.Unlock()
	require.Len(t, args, 2)
	require.Contains(t, args[1], "A 的适用条件已确认")
	require.Equal(t, "A 的适用条件已确认", state.ConfirmedSubQuestionResults[1])
	require.Len(t, messages, 2)
	require.Contains(t, messages[1].Content, "confirmed_sub_question_results")
}

func TestExecuteSubQuestionPlanStopsAtCallBudgetAndNeverUsesFailedEvidence(t *testing.T) {
	search := &recordingSearchTool{byArg: map[string]string{
		"原问题：原问题\n当前子问题：第一步":                           "第一步已确认",
		"原问题：原问题\n当前子问题：第二步\n前序已确认材料：\n[子问题1]\n第一步已确认": "第二步已确认",
	}}
	engine := newSubQuestionTestEngine(t, search)
	engine.config.SubQuestionPlan = &types.SubQuestionPlan{
		Questions: []types.SubQuestion{
			{Index: 1, Query: "第一步", Required: true},
			{Index: 2, Query: "第二步", DependsOn: []int{1}, Required: true},
			{Index: 3, Query: "第三步", DependsOn: []int{2}, Required: true},
		},
		MaxQuestions: 4, MaxModelCalls: 2, MaxDurationMs: 30000,
	}

	state := &types.AgentState{}
	messages := engine.executeSubQuestionPlan(context.Background(), "原问题", nil, state)

	search.mu.Lock()
	args := append([]string(nil), search.args...)
	search.mu.Unlock()
	require.Len(t, args, 2)
	require.Len(t, state.ConfirmedSubQuestionResults, 2)
	require.Len(t, messages, 1)
	require.NotContains(t, messages[0].Content, "第三步")
	require.NotEmpty(t, args)
}

func TestResolveSubQuestionQueryUsesOnlyOriginalAndConfirmedDependencies(t *testing.T) {
	resolved := resolveSubQuestionQuery("原问题", types.SubQuestion{
		Index: 2, Query: "它和方案 B 的差异", DependsOn: []int{1}, Required: true,
	}, map[int]string{1: "方案 A 的已确认事实"})
	if !strings.Contains(resolved, "原问题") || !strings.Contains(resolved, "方案 A 的已确认事实") {
		t.Fatalf("resolved query omitted allowed context: %q", resolved)
	}
	if strings.Contains(resolved, "子问题3") {
		t.Fatalf("resolved query included an unconfirmed dependency: %q", resolved)
	}
}
