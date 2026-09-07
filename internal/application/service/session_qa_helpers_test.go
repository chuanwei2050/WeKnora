package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type countingTenantService struct {
	interfaces.TenantService
	tenant *types.Tenant
	calls  int
}

func (s *countingTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	s.calls++
	return s.tenant, nil
}

func TestEffectiveQAConfigsLoadsTenantOnce(t *testing.T) {
	tenantService := &countingTenantService{tenant: &types.Tenant{
		RetrievalConfig: &types.RetrievalConfig{EmbeddingTopK: 17},
		ConversationConfig: &types.ConversationConfig{
			FallbackResponse: "tenant fallback",
		},
	}}
	service := &sessionService{
		cfg: &config.Config{Conversation: &config.ConversationConfig{
			FallbackStrategy: "fixed",
			FallbackPrompt:   "default prompt",
		}},
		tenantService: tenantService,
	}

	retrieval, conversation := service.effectiveQAConfigs(context.Background(), 1)

	if tenantService.calls != 1 {
		t.Fatalf("GetTenantByID calls = %d, want 1", tenantService.calls)
	}
	if retrieval.EmbeddingTopK != 17 {
		t.Fatalf("EmbeddingTopK = %d, want 17", retrieval.EmbeddingTopK)
	}
	if conversation == nil || conversation.FallbackStrategy != "fixed" || conversation.FallbackResponse != "tenant fallback" || conversation.FallbackPrompt != "default prompt" {
		t.Fatalf("conversation config = %+v", conversation)
	}
}
