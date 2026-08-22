package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidatePreflightRoleAllowsTypeInferenceForLegacyModels(t *testing.T) {
	manifest := types.ModelCapabilityManifest{}
	if err := validatePreflightRole(manifest, types.ModelRoleChat, true); err != nil {
		t.Fatalf("inferred legacy role was rejected: %v", err)
	}
}

func TestModelPreflightTimeout(t *testing.T) {
	if got := modelPreflightTimeout(0); got != 30*time.Second {
		t.Fatalf("default timeout = %v, want 30s", got)
	}
	if got := modelPreflightTimeout(45); got != 45*time.Second {
		t.Fatalf("declared timeout = %v, want 45s", got)
	}
}

func TestValidatePreflightRoleStillChecksDeclaredCapabilities(t *testing.T) {
	manifest := types.ModelCapabilityManifest{Roles: []types.ModelRole{types.ModelRoleChat}}
	if err := validatePreflightRole(manifest, types.ModelRoleChat, false); err == nil {
		t.Fatal("declared chat role without streaming capability was accepted")
	}
}

func TestModelEndpointUseDefaultsToModelRole(t *testing.T) {
	tests := map[types.ModelType]string{
		types.ModelTypeKnowledgeQA: "chat",
		types.ModelTypeEmbedding:   "embedding",
		types.ModelTypeRerank:      "rerank",
		types.ModelTypeJudge:       "judge",
		types.ModelTypeParserOCR:   "parser",
	}
	for modelType, want := range tests {
		if got := modelEndpointUseForType(modelType); got != want {
			t.Fatalf("endpoint use for %q = %q, want %q", modelType, got, want)
		}
	}
}

func TestDefaultModelEndpointUsePreservesLegacyModelAllowlist(t *testing.T) {
	if got := defaultModelEndpointUse(types.ModelTypeKnowledgeQA, types.StringArray{"model"}); got != "model" {
		t.Fatalf("legacy endpoint use = %q, want model", got)
	}
	if got := defaultModelEndpointUse(types.ModelTypeKnowledgeQA, types.StringArray{"model", "chat"}); got != "chat" {
		t.Fatalf("role-specific endpoint use = %q, want chat", got)
	}
}
