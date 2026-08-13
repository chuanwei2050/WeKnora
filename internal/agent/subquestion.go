package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	subQuestionQueryLimit   = 4000
	subQuestionOutputLimit  = 6000
	subQuestionContextLimit = 18000
)

// executeSubQuestionPlan performs the model-produced plan as a bounded,
// sequential retrieval phase. The ReAct model only receives successful tool
// results; a failed required dependency cannot silently become evidence.
func (e *AgentEngine) executeSubQuestionPlan(
	ctx context.Context,
	query string,
	messages []chat.Message,
	state *types.AgentState,
) []chat.Message {
	plan := e.config.SubQuestionPlan
	if plan == nil || len(plan.Questions) <= 1 || e.toolRegistry == nil {
		return messages
	}
	if err := plan.Validate(); err != nil {
		return messages
	}
	if _, err := e.toolRegistry.GetTool(agenttools.ToolKnowledgeSearch); err != nil {
		common.PipelineWarn(ctx, "Agent", "subquestion_plan_skipped", map[string]interface{}{"reason": "knowledge_search_unavailable"})
		return messages
	}

	planCtx, cancel := context.WithTimeout(ctx, time.Duration(plan.MaxDurationMs)*time.Millisecond)
	defer cancel()
	confirmed := make(map[int]string, len(plan.Questions))
	queries := make(map[int]string, len(plan.Questions))
	calls := 0
	stoppedReason := "completed"

	for _, question := range plan.Questions {
		if err := planCtx.Err(); err != nil {
			stoppedReason = "duration_budget_exhausted"
			break
		}
		if calls >= plan.MaxModelCalls {
			stoppedReason = "model_call_budget_exhausted"
			break
		}
		dependenciesReady := true
		for _, dependency := range question.DependsOn {
			if _, ok := confirmed[dependency]; !ok {
				dependenciesReady = false
				break
			}
		}
		if !dependenciesReady {
			if question.Required {
				stoppedReason = fmt.Sprintf("required_dependency_%d_missing", question.Index)
				break
			}
			continue
		}

		resolvedQuery := resolveSubQuestionQuery(query, question, confirmed)
		queries[question.Index] = resolvedQuery
		args, _ := json.Marshal(map[string]interface{}{"queries": []string{resolvedQuery}})
		calls++
		result, err := e.toolRegistry.ExecuteTool(planCtx, agenttools.ToolKnowledgeSearch, args)
		if err != nil || result == nil || !result.Success {
			if question.Required {
				stoppedReason = fmt.Sprintf("required_question_%d_failed", question.Index)
				break
			}
			continue
		}
		confirmed[question.Index] = truncateRunes(result.Output, subQuestionOutputLimit)
	}

	if len(confirmed) == 0 {
		common.PipelineWarn(ctx, "Agent", "subquestion_plan_finished", map[string]interface{}{
			"planned": len(plan.Questions), "calls": calls, "confirmed": 0, "reason": stoppedReason,
		})
		return messages
	}
	if state.ConfirmedSubQuestionResults == nil {
		state.ConfirmedSubQuestionResults = make(map[int]string, len(confirmed))
	}
	for index, output := range confirmed {
		state.ConfirmedSubQuestionResults[index] = output
	}

	var evidence strings.Builder
	evidence.WriteString(`<confirmed_sub_question_results execution="ordered" source="knowledge_search">`)
	for _, question := range plan.Questions {
		output, ok := confirmed[question.Index]
		if !ok {
			continue
		}
		evidence.WriteString(fmt.Sprintf(`<result index="%d" query="%s">%s</result>`,
			question.Index, html.EscapeString(queries[question.Index]), html.EscapeString(output)))
		if evidence.Len() >= subQuestionContextLimit {
			break
		}
	}
	evidence.WriteString(`</confirmed_sub_question_results>`)
	messages = append(messages, chat.Message{
		Role:    "user",
		Content: "以下是按依赖顺序完成且已确认成功的检索材料。后续回答只能将其作为证据，不能把未成功的子问题当作已完成：\n" + truncateRunes(evidence.String(), subQuestionContextLimit),
	})
	common.PipelineInfo(ctx, "Agent", "subquestion_plan_finished", map[string]interface{}{
		"planned": len(plan.Questions), "calls": calls, "confirmed": len(confirmed), "reason": stoppedReason,
	})
	return messages
}

func resolveSubQuestionQuery(original string, question types.SubQuestion, confirmed map[int]string) string {
	var builder strings.Builder
	builder.WriteString("原问题：")
	builder.WriteString(truncateRunes(strings.TrimSpace(original), subQuestionQueryLimit))
	builder.WriteString("\n当前子问题：")
	builder.WriteString(truncateRunes(strings.TrimSpace(question.Query), subQuestionQueryLimit))
	if len(question.DependsOn) == 0 {
		return builder.String()
	}
	builder.WriteString("\n前序已确认材料：")
	for _, dependency := range question.DependsOn {
		if output, ok := confirmed[dependency]; ok {
			builder.WriteString(fmt.Sprintf("\n[子问题%d]\n%s", dependency, truncateRunes(output, subQuestionOutputLimit)))
		}
	}
	return truncateRunes(builder.String(), subQuestionContextLimit)
}
