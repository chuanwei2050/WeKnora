package agent

import (
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestCanSynthesizeFinalAnswerRequiresDeepReadAfterSearch(t *testing.T) {
	state := &types.AgentState{RoundSteps: []types.AgentStep{{ToolCalls: []types.ToolCall{
		{Name: agenttools.ToolGrepChunks, Result: &types.ToolResult{Success: true, Output: "matches"}},
		{Name: agenttools.ToolKnowledgeSearch, Result: &types.ToolResult{Success: true, Output: "candidates"}},
	}}}}

	if canSynthesizeFinalAnswer(state) {
		t.Fatal("search-only evidence must not be synthesized into a customer-visible final answer")
	}

	state.RoundSteps = append(state.RoundSteps, types.AgentStep{ToolCalls: []types.ToolCall{
		{Name: agenttools.ToolListKnowledgeChunks, Result: &types.ToolResult{Success: true, Output: "full record"}},
	}})
	if !canSynthesizeFinalAnswer(state) {
		t.Fatal("deep-read evidence should allow final-answer synthesis")
	}
}

func TestBuildFinalAnswerPromptUsesPublicOutputContract(t *testing.T) {
	prompt := buildFinalAnswerPrompt("谁持有证书？")

	if strings.Contains(prompt, "Clearly cite information sources (chunk_id") {
		t.Fatal("fallback prompt must not request internal chunk identifiers")
	}
	for _, required := range []string{"只输出面向用户的最终答案", "不要输出分析过程", "谁持有证书？"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("fallback prompt missing public-output requirement %q", required)
		}
	}
}
