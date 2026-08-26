package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

type stageEventBus struct {
	handlers map[types.EventType][]types.EventHandler
}

func (b *stageEventBus) On(eventType types.EventType, handler types.EventHandler) {
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *stageEventBus) Emit(ctx context.Context, evt types.Event) error {
	for _, handler := range b.handlers[evt.Type] {
		if err := handler(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func TestPipelineStageUsesExistingAGUIEventContract(t *testing.T) {
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
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{SessionID: "session-1"}, PipelineContext: types.PipelineContext{EventBus: bus}}

	id, started := emitPipelineStageStart(context.Background(), manage, "rerank", "筛选相关内容")
	emitPipelineStageResult(context.Background(), manage, id, "rerank", "筛选完成", started, true)

	if id == "" || call.ToolCallID != id || call.ToolName != "rerank" || call.Hint != "筛选相关内容" {
		t.Fatalf("unexpected tool call: %#v", call)
	}
	if result.ToolCallID != id || result.ToolName != "rerank" || !result.Success || result.Output != "筛选完成" {
		t.Fatalf("unexpected tool result: %#v", result)
	}
}
