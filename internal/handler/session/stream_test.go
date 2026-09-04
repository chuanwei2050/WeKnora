package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type titleBeforeCompleteStreamManager struct{}

func (titleBeforeCompleteStreamManager) AppendEvent(context.Context, string, string, interfaces.StreamEvent) error {
	return nil
}

func (titleBeforeCompleteStreamManager) GetEvents(_ context.Context, _, _ string, fromOffset int) ([]interfaces.StreamEvent, int, error) {
	switch fromOffset {
	case 0:
		return []interfaces.StreamEvent{{Type: types.ResponseTypeSessionTitle, Content: "测试标题", Done: true}}, 1, nil
	case 1:
		return []interfaces.StreamEvent{{Type: types.ResponseTypeComplete, Done: true}}, 2, nil
	default:
		return nil, fromOffset, nil
	}
}

func TestRemainingTitleDeliveryWait(t *testing.T) {
	startedAt := time.Unix(0, 0)

	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{name: "full window", now: startedAt, want: 3 * time.Second},
		{name: "remaining window", now: startedAt.Add(2400 * time.Millisecond), want: 600 * time.Millisecond},
		{name: "window elapsed", now: startedAt.Add(3 * time.Second), want: 0},
		{name: "window exceeded", now: startedAt.Add(5 * time.Second), want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := remainingTitleDeliveryWait(startedAt, test.now); got != test.want {
				t.Fatalf("remainingTitleDeliveryWait() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsTerminalStreamEvent(t *testing.T) {
	tests := []struct {
		name  string
		event interfaces.StreamEvent
		want  bool
	}{
		{name: "complete", event: interfaces.StreamEvent{Type: types.ResponseTypeComplete}, want: true},
		{name: "stop", event: interfaces.StreamEvent{Type: types.ResponseType(event.EventStop), Done: true}, want: true},
		{name: "fatal error", event: interfaces.StreamEvent{Type: types.ResponseTypeError, Done: true}, want: true},
		{name: "answer done", event: interfaces.StreamEvent{Type: types.ResponseTypeAnswer, Done: true}, want: true},
		{name: "answer delta", event: interfaces.StreamEvent{Type: types.ResponseTypeAnswer}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isTerminalStreamEvent(test.event); got != test.want {
				t.Fatalf("isTerminalStreamEvent() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBuildStreamResponseCarriesAssistantMessageID(t *testing.T) {
	response := buildStreamResponse(
		interfaces.StreamEvent{Type: types.ResponseTypeAnswer, Content: "答案", Done: true},
		"request-1",
		"assistant-1",
	)

	if response.AssistantMessageID != "assistant-1" {
		t.Fatalf("AssistantMessageID = %q, want %q", response.AssistantMessageID, "assistant-1")
	}
}

func TestHandleAgentEventsDoesNotWaitWhenTitleArrivedBeforeComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	handler := &Handler{streamManager: titleBeforeCompleteStreamManager{}}

	startedAt := time.Now()
	handler.handleAgentEventsForSSE(
		context.Background(), c, "session-1", "message-1", "request-1", nil, true,
	)

	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("handler waited %v after title had already arrived", elapsed)
	}
}
