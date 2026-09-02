package service

import (
	"context"
	"errors"
	"testing"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type captureSearchTenantPlugin struct {
	tenantIDs []uint64
}

func (p *captureSearchTenantPlugin) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH, types.CHUNK_RERANK, types.CHUNK_MERGE, types.FILTER_TOP_K}
}

func (p *captureSearchTenantPlugin) OnEvent(
	_ context.Context,
	_ types.EventType,
	chatManage *types.ChatManage,
	next func() *chatpipeline.PluginError,
) *chatpipeline.PluginError {
	p.tenantIDs = append(p.tenantIDs, chatManage.TenantID)
	return next()
}

type unavailableModelService struct {
	interfaces.ModelService
}

func (unavailableModelService) GetDefaultModel(context.Context, types.ModelType, string) (*types.Model, error) {
	return nil, errors.New("no default model")
}

func TestSearchKnowledgePropagatesTenantIDToPipeline(t *testing.T) {
	events := chatpipeline.NewEventManager()
	capture := &captureSearchTenantPlugin{}
	events.Register(capture)
	service := &sessionService{
		cfg:                  &config.Config{Conversation: &config.ConversationConfig{}},
		knowledgeBaseService: searchTargetKnowledgeBaseService{items: []*types.KnowledgeBase{{ID: "kb-1", TenantID: 10001}}},
		knowledgeService:     searchTargetKnowledgeService{},
		modelService:         unavailableModelService{},
		eventManager:         events,
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(10001))

	_, err := service.SearchKnowledge(ctx, []string{"kb-1"}, nil, false, "团队有CISP")

	require.NoError(t, err)
	require.Equal(t, []uint64{10001, 10001, 10001, 10001}, capture.tenantIDs)
}
