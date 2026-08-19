package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

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
