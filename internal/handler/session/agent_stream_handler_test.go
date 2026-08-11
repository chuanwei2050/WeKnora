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
