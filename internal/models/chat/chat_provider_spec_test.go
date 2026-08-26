package chat

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/stretchr/testify/require"
)

func TestSiliconFlowQwen3RequestDisablesThinking(t *testing.T) {
	spec := findProviderSpec(provider.ProviderSiliconFlow, "Qwen/Qwen3.6-27B")
	require.NotNil(t, spec)

	req := newTestRemoteChat(t).BuildChatCompletionRequest(nil, nil, true)
	custom, useRawHTTP := spec.RequestCustomizer(&req, nil, true)
	require.True(t, useRawHTTP)

	payload, err := json.Marshal(custom)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Equal(t, false, decoded["enable_thinking"])
}

func TestNonQwenSiliconFlowDoesNotUseQwenThinkingCustomizer(t *testing.T) {
	spec := findProviderSpec(provider.ProviderSiliconFlow, "deepseek-ai/DeepSeek-V3.1-Terminus")
	require.Nil(t, spec)
}

func TestDeepSeekRequestDisablesThinking(t *testing.T) {
	spec := findProviderSpec(provider.ProviderDeepSeek, "deepseek-v4-flash-vision-exp")
	require.NotNil(t, spec)

	thinking := false
	req := newTestRemoteChat(t).BuildChatCompletionRequest(nil, &ChatOptions{Thinking: &thinking}, false)
	custom, useRawHTTP := spec.RequestCustomizer(&req, &ChatOptions{Thinking: &thinking}, false)
	require.True(t, useRawHTTP)

	payload, err := json.Marshal(custom)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	require.Equal(t, map[string]any{"type": "disabled"}, decoded["thinking"])
}
