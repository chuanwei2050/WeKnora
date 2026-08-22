package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type platformSettingsTenantService struct {
	interfaces.TenantService
	settings *types.PlatformSettings
}

func (s platformSettingsTenantService) GetPlatformSettings(context.Context) (*types.PlatformSettings, error) {
	return s.settings, nil
}

func TestSelectDefaultModelUsesActiveProfileRoleDefault(t *testing.T) {
	models := []*types.Model{
		{ID: "online-first", Type: types.ModelTypeKnowledgeQA, Profile: types.ModelProfileOnline, ProfileRole: "chat", Status: types.ModelStatusActive},
		{ID: "online-default", Type: types.ModelTypeKnowledgeQA, Profile: types.ModelProfileOnline, ProfileRole: "chat", Status: types.ModelStatusActive, IsDefault: true},
		{ID: "offline-default", Type: types.ModelTypeKnowledgeQA, Profile: types.ModelProfileOffline, ProfileRole: "chat", Status: types.ModelStatusActive, IsDefault: true},
	}

	selected := selectDefaultModel(models, types.ModelProfileOnline, types.ModelTypeKnowledgeQA, "chat")
	if selected == nil || selected.ID != "online-default" {
		t.Fatalf("selected=%v, want online-default", selected)
	}
}

func TestClearPlatformManagedModelIDs(t *testing.T) {
	config := types.CustomAgentConfig{
		ModelID:       "chat",
		RerankModelID: "rerank",
		VLMModelID:    "vlm",
		ASRModelID:    "asr",
		TTSModelID:    "tts",
		VerifiedAnswer: types.VerifiedAnswerConfig{
			FactValidatorModelID:     "fact",
			LogicValidatorModelID:    "logic",
			CitationValidatorModelID: "citation",
		},
	}

	clearPlatformManagedModelIDs(&config)
	if config.ModelID != "" || config.RerankModelID != "" || config.VLMModelID != "" || config.ASRModelID != "" || config.TTSModelID != "" {
		t.Fatalf("platform-managed model IDs were not cleared: %+v", config)
	}
	if config.VerifiedAnswer.FactValidatorModelID != "" || config.VerifiedAnswer.LogicValidatorModelID != "" || config.VerifiedAnswer.CitationValidatorModelID != "" {
		t.Fatalf("verification model IDs were not cleared: %+v", config.VerifiedAnswer)
	}
}

func TestResolveEmbeddingModelIDUsesActivePlatformDefault(t *testing.T) {
	repo := &duplicateModelRepository{models: []*types.Model{{
		ID:          "offline-embedding",
		Type:        types.ModelTypeEmbedding,
		Profile:     types.ModelProfileOffline,
		ProfileRole: "embedding",
		Status:      types.ModelStatusActive,
		IsDefault:   true,
	}}}
	service := &modelService{
		repo: repo,
		tenantService: platformSettingsTenantService{settings: &types.PlatformSettings{
			ModelProfile: types.ModelProfileOffline,
		}},
	}

	modelID, err := service.resolveEmbeddingModelID(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if modelID != "offline-embedding" {
		t.Fatalf("model ID = %q, want offline-embedding", modelID)
	}
}
