package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestApplyAgentOverridesDoesNotChangePlatformRetrievalStrategy(t *testing.T) {
	platform := types.DefaultRetrievalConfig()
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		EmbeddingTopK:        platform.EmbeddingTopK,
		VectorRecallTopK:     platform.VectorRecallTopK,
		KeywordRecallTopK:    platform.KeywordRecallTopK,
		RRFVectorWeight:      platform.RRFVectorWeight,
		VectorThreshold:      platform.VectorThreshold,
		KeywordThreshold:     platform.KeywordThreshold,
		RerankCandidateTopK:  platform.RerankCandidateTopK,
		RerankTopK:           platform.RerankTopK,
		RerankThreshold:      platform.RerankThreshold,
		EnableQueryExpansion: true,
		FallbackStrategy:     types.FallbackStrategyFixed,
		FallbackResponse:     "platform response",
		FallbackPrompt:       "platform prompt",
	}}
	agent := &types.CustomAgent{Config: types.CustomAgentConfig{
		EmbeddingTopK:        1,
		VectorRecallTopK:     2,
		KeywordRecallTopK:    3,
		RRFVectorWeight:      0.1,
		VectorThreshold:      0.9,
		KeywordThreshold:     0.8,
		RerankCandidateTopK:  1,
		RerankTopK:           1,
		RerankThreshold:      9,
		EnableQueryExpansion: false,
		FallbackStrategy:     "model",
		FallbackResponse:     "agent response",
		FallbackPrompt:       "agent prompt",
	}}

	(&sessionService{}).applyAgentOverridesToChatManage(context.Background(), agent, manage)

	require.Equal(t, platform.EmbeddingTopK, manage.EmbeddingTopK)
	require.Equal(t, platform.VectorRecallTopK, manage.VectorRecallTopK)
	require.Equal(t, platform.KeywordRecallTopK, manage.KeywordRecallTopK)
	require.Equal(t, platform.RRFVectorWeight, manage.RRFVectorWeight)
	require.Equal(t, platform.VectorThreshold, manage.VectorThreshold)
	require.Equal(t, platform.KeywordThreshold, manage.KeywordThreshold)
	require.Equal(t, platform.RerankCandidateTopK, manage.RerankCandidateTopK)
	require.Equal(t, platform.RerankTopK, manage.RerankTopK)
	require.Equal(t, platform.RerankThreshold, manage.RerankThreshold)
	require.True(t, manage.EnableQueryExpansion)
	require.Equal(t, types.FallbackStrategyFixed, manage.FallbackStrategy)
	require.Equal(t, "platform response", manage.FallbackResponse)
	require.Equal(t, "platform prompt", manage.FallbackPrompt)
}
