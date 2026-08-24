package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

type recordingEventBus struct {
	events []types.Event
}

func (b *recordingEventBus) On(types.EventType, types.EventHandler) {}

func (b *recordingEventBus) Emit(_ context.Context, evt types.Event) error {
	b.events = append(b.events, evt)
	return nil
}

func TestVerifiedStreamPublishesOnlyFinalAnswerAfterValidation(t *testing.T) {
	bus := &recordingEventBus{}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			TenantID:  7,
			SessionID: "session-1",
			Query:     "question",
			VerifiedAnswer: types.VerifiedAnswerConfig{
				Enabled: true,
			},
		},
		PipelineState: types.PipelineState{
			SearchResult: []*types.SearchResult{{
				ID:          "chunk-1",
				Content:     "supporting source",
				KnowledgeID: "knowledge-1",
			}},
		},
		PipelineContext: types.PipelineContext{EventBus: bus},
	}

	plugin := &PluginChatCompletionStream{}
	plugin.emitVerifiedResult(context.Background(), bus, "answer-1", chatManage, "draft answer")

	finalAnswers := 0
	for _, evt := range bus.events {
		if evt.Type != types.EventType(event.EventAgentFinalAnswer) {
			continue
		}
		finalAnswers++
		data, ok := evt.Data.(event.AgentFinalAnswerData)
		if !ok || !data.Done || data.Content != "draft answer" {
			t.Fatalf("unexpected final verified event: %#v", evt)
		}
	}
	if finalAnswers != 1 {
		t.Fatalf("verified stream emitted %d answer events, want exactly one final event", finalAnswers)
	}
	for _, evt := range bus.events {
		if evt.Type != types.EventType(event.EventAgentReflection) {
			continue
		}
		if _, ok := evt.Data.(types.VerificationStageEvent); !ok {
			t.Fatalf("verification event has unexpected payload: %#v", evt.Data)
		}
	}
}

func TestVerifiedVisibleTextIncludesConservativeNote(t *testing.T) {
	answer := &types.VerifiedAnswer{
		Text:             "candidate",
		Degraded:         true,
		ConservativeNote: "请人工核验。",
	}
	if got := verifiedVisibleText(answer); got != "请人工核验。\n\ncandidate" {
		t.Fatalf("visible conservative answer = %q", got)
	}
}

func TestThinkingVisibilityHonorsExplicitModelSetting(t *testing.T) {
	enabled := true
	disabled := false
	if !shouldExposeThinking(nil) || !shouldExposeThinking(&enabled) || shouldExposeThinking(&disabled) {
		t.Fatal("thinking visibility did not honor the explicit model setting")
	}
}

func TestVerifiedAnswerDisabledPreservesDefaultCompletion(t *testing.T) {
	chatManage := &types.ChatManage{
		PipelineState: types.PipelineState{ChatResponse: &types.ChatResponse{Content: "ordinary answer"}},
	}
	nextCalled := false
	plugin := &PluginVerifiedAnswer{}
	if err := plugin.OnEvent(context.Background(), types.CHAT_COMPLETION, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !nextCalled || chatManage.ChatResponse.Content != "ordinary answer" || chatManage.VerifiedResult != nil {
		t.Fatalf("default completion was changed: next=%v response=%+v verified=%+v", nextCalled, chatManage.ChatResponse, chatManage.VerifiedResult)
	}
}

func TestVerifiedAnswerFailureDoesNotPublishDraft(t *testing.T) {
	chatManage := &types.ChatManage{
		PipelineState: types.PipelineState{ChatResponse: &types.ChatResponse{Content: "unverified draft"}},
		PipelineRequest: types.PipelineRequest{
			TenantID:  7,
			SessionID: "session-1",
			VerifiedAnswer: types.VerifiedAnswerConfig{
				Enabled: true,
			},
		},
	}
	nextCalled := false
	plugin := &PluginVerifiedAnswer{}
	if err := plugin.OnEvent(context.Background(), types.CHAT_COMPLETION, chatManage, func() *PluginError {
		nextCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !nextCalled || chatManage.ChatResponse.Content == "unverified draft" || chatManage.ChatResponse.Content == "" {
		t.Fatalf("verification failure leaked draft: next=%v content=%q", nextCalled, chatManage.ChatResponse.Content)
	}
}
