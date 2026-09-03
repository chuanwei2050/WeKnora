package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type defaultVLMModelService struct {
	interfaces.ModelService
	defaultModel *types.Model
	requested    bool
}

func (s *defaultVLMModelService) GetDefaultModel(context.Context, types.ModelType, string) (*types.Model, error) {
	s.requested = true
	return s.defaultModel, nil
}

func TestGetVLMConfigUsesCurrentPlatformDefaultDespiteStaleKBModelID(t *testing.T) {
	models := &defaultVLMModelService{defaultModel: &types.Model{
		ID:   "current-default",
		Name: "vision-current",
		Parameters: types.ModelParameters{
			BaseURL:       "https://current.example/v1",
			APIKey:        "current-key",
			InterfaceType: "openai-compatible",
		},
	}}
	service := &knowledgeService{modelService: models}
	kb := &types.KnowledgeBase{VLMConfig: types.VLMConfig{
		Enabled: true,
		ModelID: "deleted-old-model",
	}}

	config, err := service.getVLMConfig(context.Background(), kb)
	if err != nil {
		t.Fatal(err)
	}
	if !models.requested {
		t.Fatal("expected current platform default VLM to be resolved")
	}
	if config.ModelName != "vision-current" || config.BaseURL != "https://current.example/v1" {
		t.Fatalf("unexpected VLM config: %+v", config)
	}
}

func TestGetVLMConfigPreservesLegacyInlineConfiguration(t *testing.T) {
	models := &defaultVLMModelService{}
	service := &knowledgeService{modelService: models}
	kb := &types.KnowledgeBase{VLMConfig: types.VLMConfig{
		ModelName: "legacy-vision",
		BaseURL:   "https://legacy.example/v1",
	}}

	config, err := service.getVLMConfig(context.Background(), kb)
	if err != nil {
		t.Fatal(err)
	}
	if models.requested {
		t.Fatal("legacy inline configuration should not resolve a platform default")
	}
	if config.ModelName != "legacy-vision" {
		t.Fatalf("unexpected legacy VLM config: %+v", config)
	}
}
