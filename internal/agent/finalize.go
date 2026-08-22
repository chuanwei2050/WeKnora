package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

var errIncompleteKnowledgeEvidence = errors.New("cannot synthesize final answer before deep-reading retrieved knowledge")

func canSynthesizeFinalAnswer(state *types.AgentState) bool {
	searchSucceeded := false
	deepReadSucceeded := false
	for _, step := range state.RoundSteps {
		for _, toolCall := range step.ToolCalls {
			if toolCall.Result == nil || !toolCall.Result.Success || toolCall.Result.Output == "" {
				continue
			}
			switch toolCall.Name {
			case agenttools.ToolKnowledgeSearch, agenttools.ToolGrepChunks:
				searchSucceeded = true
			case agenttools.ToolListKnowledgeChunks:
				deepReadSucceeded = true
			}
		}
	}
	return !searchSucceeded || deepReadSucceeded
}

func buildFinalAnswerPrompt(query string) string {
	return fmt.Sprintf(`Based only on the retrieved evidence above, answer the user's question.

User question: %s

Requirements:
1. Only state conclusions supported by the retrieved evidence. If evidence is insufficient, say so plainly.
2. Cite sources only by their user-facing document names when useful. Never expose internal identifiers, raw retrieval payloads, tool names, or system metadata.
3. Output only the user-facing final answer. Do not output analysis, plans, retrieval steps, self-talk, or a restatement of the user's intent.
4. Keep the answer concise, structured, and in the same language as the user's question.

只输出面向用户的最终答案，不要输出分析过程、检索过程或任何内部标识。`, query)
}

// streamFinalAnswerToEventBus streams the final answer generation through EventBus
func (e *AgentEngine) streamFinalAnswerToEventBus(
	ctx context.Context,
	query string,
	state *types.AgentState,
	sessionID string,
) error {
	if !canSynthesizeFinalAnswer(state) {
		logger.Warnf(ctx, "[Agent][FinalAnswer] Refusing synthesis because search results were not deep-read")
		common.PipelineWarn(ctx, "Agent", "final_answer_incomplete_evidence", map[string]interface{}{
			"session_id": sessionID,
		})
		return errIncompleteKnowledgeEvidence
	}

	totalToolCalls := countTotalToolCalls(state.RoundSteps)
	logger.Infof(ctx, "[Agent][FinalAnswer] Synthesizing from %d steps, %d tool calls",
		len(state.RoundSteps), totalToolCalls)
	common.PipelineInfo(ctx, "Agent", "final_answer_start", map[string]interface{}{
		"session_id":   sessionID,
		"query":        query,
		"steps":        len(state.RoundSteps),
		"tool_results": totalToolCalls,
	})

	// Build messages with all context
	language := types.LanguageNameFromContext(ctx)
	systemPrompt := BuildSystemPromptWithOptions(
		e.knowledgeBasesInfo,
		e.config.WebSearchEnabled,
		e.selectedDocs,
		&BuildSystemPromptOptions{
			Language: language,
			Config:   e.appConfig,
		},
		e.systemPromptTemplate,
	)

	messages := []chat.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: query},
	}

	// Add all tool call results as context
	toolResultCount := 0
	for stepIdx, step := range state.RoundSteps {
		for toolIdx, toolCall := range step.ToolCalls {
			toolResultCount++
			messages = append(messages, chat.Message{
				Role:    "user",
				Content: fmt.Sprintf("Retrieved evidence %d: %s", toolResultCount, toolCall.Result.Output),
			})
			logger.Debugf(ctx, "[Agent][FinalAnswer] Added tool result [Step-%d][Tool-%d]: %s (output: %d chars)",
				stepIdx+1, toolIdx+1, toolCall.Name, len(toolCall.Result.Output))
		}
	}

	logger.Debugf(ctx, "[Agent][FinalAnswer] Built context: %d messages, %d tool results",
		len(messages), toolResultCount)

	messages = append(messages, chat.Message{
		Role:    "user",
		Content: buildFinalAnswerPrompt(query),
	})

	// Generate a single ID for this entire final answer stream
	answerID := generateEventID("answer")
	logger.Debugf(ctx, "[Agent][FinalAnswer] AnswerID: %s", answerID)

	llmResult, err := e.streamLLMToEventBus(
		ctx,
		messages,
		&chat.ChatOptions{Temperature: e.config.Temperature, Thinking: e.config.Thinking},
		nil,
	)
	if err != nil {
		logger.Errorf(ctx, "[Agent][FinalAnswer] Final answer generation failed: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_stream_failed", map[string]interface{}{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return err
	}

	fullAnswer := llmResult.Content
	if fullAnswer == "" {
		return errors.New("final answer synthesis returned empty content")
	}
	// Fallback synthesis is buffered so downstream consumers can validate the
	// complete customer-visible answer before any portion is displayed.
	e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data: event.AgentFinalAnswerData{
			Content:        fullAnswer,
			Done:           false,
			OutputContract: event.AgentFinalAnswerOutputContract,
		},
	})
	e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data: event.AgentFinalAnswerData{
			Content:        "",
			Done:           true,
			OutputContract: event.AgentFinalAnswerOutputContract,
		},
	})
	logger.Infof(ctx, "[Agent][FinalAnswer] Final answer generated: %d characters", len(fullAnswer))
	common.PipelineInfo(ctx, "Agent", "final_answer_done", map[string]interface{}{
		"session_id": sessionID,
		"answer_len": len(fullAnswer),
	})
	state.FinalAnswer = fullAnswer
	return nil
}

// handleMaxIterations generates a final answer when the agent loop exhausted all iterations
// without the LLM producing a natural stop. It marks state.IsComplete = true.
func (e *AgentEngine) handleMaxIterations(
	ctx context.Context, query string, state *types.AgentState, sessionID string,
) {
	logger.Info(ctx, "Reached max iterations, generating final answer")
	common.PipelineWarn(ctx, "Agent", "max_iterations_reached", map[string]interface{}{
		"iterations": state.CurrentRound,
		"max":        e.config.MaxIterations,
	})

	// Stream final answer generation through EventBus
	if err := e.streamFinalAnswerToEventBus(ctx, query, state, sessionID); err != nil {
		logger.Errorf(ctx, "Failed to synthesize final answer: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_failed", map[string]interface{}{
			"error": err.Error(),
		})
		state.FinalAnswer = "Sorry, I was unable to generate a complete answer."
	}
	state.IsComplete = true
}

// emitCompletionEvent emits the EventAgentComplete event with execution summary.
func (e *AgentEngine) emitCompletionEvent(
	ctx context.Context, state *types.AgentState, sessionID, messageID string, startTime time.Time,
) {
	// Convert knowledge refs to interface{} slice for event data
	knowledgeRefsInterface := make([]interface{}, 0, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		knowledgeRefsInterface = append(knowledgeRefsInterface, ref)
	}

	extra := map[string]interface{}{"execution_path": "agent"}
	if e.config != nil && e.config.RoutingDecision != nil {
		extra["routing"] = e.config.RoutingDecision.Summary()
	}
	if e.config != nil && e.config.SubQuestionPlan != nil {
		extra["sub_question_plan"] = e.config.SubQuestionPlan
	}
	e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("complete"),
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data: event.AgentCompleteData{
			FinalAnswer:     state.FinalAnswer,
			KnowledgeRefs:   knowledgeRefsInterface,
			AgentSteps:      state.RoundSteps, // Include detailed execution steps for message storage
			TotalSteps:      len(state.RoundSteps),
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			MessageID:       messageID, // Include message ID for proper message update
			Extra:           extra,
		},
	})

	logger.Infof(ctx, "Agent execution completed in %d rounds", state.CurrentRound)
}
