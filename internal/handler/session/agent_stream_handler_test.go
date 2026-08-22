package session

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type recordingStreamManager struct {
	events []interfaces.StreamEvent
}

func (m *recordingStreamManager) AppendEvent(
	_ context.Context,
	_, _ string,
	streamEvent interfaces.StreamEvent,
) error {
	m.events = append(m.events, streamEvent)
	return nil
}

func (m *recordingStreamManager) GetEvents(
	_ context.Context,
	_, _ string,
	_ int,
) ([]interfaces.StreamEvent, int, error) {
	return m.events, len(m.events), nil
}

func TestHandleToolResultKeepsFailureNonFatal(t *testing.T) {
	streamManager := &recordingStreamManager{}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		streamManager:      streamManager,
		eventStartTimes:    map[string]time.Time{},
	}

	err := handler.handleToolResult(context.Background(), event.Event{
		ID: "event-1",
		Data: event.AgentToolResultData{
			ToolCallID: "call-1",
			ToolName:   "knowledge_search",
			Success:    false,
			Error:      "no search targets available",
		},
	})

	if err != nil {
		t.Fatalf("handleToolResult() error = %v", err)
	}
	if len(streamManager.events) != 1 {
		t.Fatalf("events = %d, want 1", len(streamManager.events))
	}
	got := streamManager.events[0]
	if got.Type != types.ResponseTypeToolResult {
		t.Fatalf("event type = %q, want %q", got.Type, types.ResponseTypeToolResult)
	}
	if got.Data["success"] != false || got.Data["error"] != "no search targets available" {
		t.Fatalf("event data = %#v", got.Data)
	}
}

func TestHandleFinalAnswerAddsStructuredOutputContract(t *testing.T) {
	streamManager := &recordingStreamManager{}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		assistantMessage:   &types.Message{},
		streamManager:      streamManager,
		eventStartTimes:    map[string]time.Time{},
	}

	err := handler.handleFinalAnswer(context.Background(), event.Event{
		ID: "answer-1",
		Data: event.AgentFinalAnswerData{
			Content:        "正式答案",
			Done:           false,
			OutputContract: event.AgentFinalAnswerOutputContract,
		},
	})

	if err != nil {
		t.Fatalf("handleFinalAnswer() error = %v", err)
	}
	if len(streamManager.events) != 1 {
		t.Fatalf("events = %d, want 1", len(streamManager.events))
	}
	got := streamManager.events[0]
	if got.Data["output_contract"] != "agent_final_answer/v1" {
		t.Fatalf("output contract = %#v", got.Data["output_contract"])
	}
}

func TestHandleFinalAnswerDoesNotInventOutputContract(t *testing.T) {
	streamManager := &recordingStreamManager{}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		assistantMessage:   &types.Message{},
		streamManager:      streamManager,
		eventStartTimes:    map[string]time.Time{},
	}

	err := handler.handleFinalAnswer(context.Background(), event.Event{
		ID: "answer-1",
		Data: event.AgentFinalAnswerData{
			Content: "自然停止内容",
			Done:    false,
		},
	})

	if err != nil {
		t.Fatalf("handleFinalAnswer() error = %v", err)
	}
	if got := streamManager.events[0].Data["output_contract"]; got != nil {
		t.Fatalf("output contract = %#v, want nil", got)
	}
}

func TestHandleReflectionForwardsVerificationStage(t *testing.T) {
	streamManager := &recordingStreamManager{}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		streamManager:      streamManager,
	}

	err := handler.handleReflection(context.Background(), event.Event{
		ID: "verification-1",
		Data: types.VerificationStageEvent{
			Stage:     "verification",
			Status:    "completed",
			Decision:  types.VerificationPassed,
			ModelKeys: []string{"27b", "9b"},
		},
	})
	if err != nil {
		t.Fatalf("handleReflection() error = %v", err)
	}
	if len(streamManager.events) != 1 {
		t.Fatalf("events = %d, want 1", len(streamManager.events))
	}
	got := streamManager.events[0]
	if got.Type != types.ResponseTypeReflection || !got.Done {
		t.Fatalf("event = %#v, want completed reflection", got)
	}
	if got.Data["stage"] != "verification" || got.Data["status"] != "completed" || got.Data["decision"] != string(types.VerificationPassed) {
		t.Fatalf("event data = %#v", got.Data)
	}
}

func TestHandleErrorRemainsFatal(t *testing.T) {
	streamManager := &recordingStreamManager{}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		streamManager:      streamManager,
	}

	err := handler.handleError(context.Background(), event.Event{
		ID: "event-1",
		Data: event.ErrorData{
			Stage: "agent_execution",
			Error: "model unavailable",
		},
	})

	if err != nil {
		t.Fatalf("handleError() error = %v", err)
	}
	if len(streamManager.events) != 1 || streamManager.events[0].Type != types.ResponseTypeError {
		t.Fatalf("events = %#v, want one fatal error", streamManager.events)
	}
}

func TestAgentStreamHandlerPersistsUserVisibleResponseTiming(t *testing.T) {
	streamManager := &recordingStreamManager{}
	acceptedAt := time.Now().UTC().Add(-time.Second)
	message := &types.Message{ID: "message-1"}
	handler := &AgentStreamHandler{
		ctx:                context.Background(),
		sessionID:          "session-1",
		assistantMessageID: "message-1",
		assistantMessage:   message,
		streamManager:      streamManager,
		eventStartTimes:    map[string]time.Time{},
		acceptedAt:         acceptedAt,
	}
	if err := handler.handleFinalAnswer(context.Background(), event.Event{ID: "answer-1", Data: event.AgentFinalAnswerData{Content: "visible", Done: false}}); err != nil {
		t.Fatal(err)
	}
	if err := handler.handleComplete(context.Background(), event.Event{ID: "complete-1", Data: event.AgentCompleteData{MessageID: "message-1"}}); err != nil {
		t.Fatal(err)
	}
	timing, err := message.ResponseTiming.Map()
	if err != nil {
		t.Fatal(err)
	}
	if timing["accepted_at"] == nil || timing["first_visible_at"] == nil || timing["completed_at"] == nil {
		t.Fatalf("response timing missing lifecycle timestamps: %#v", timing)
	}
}
