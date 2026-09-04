package service

import (
	"bytes"
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

func TestVisionChallengeCountUsesRandomInput(t *testing.T) {
	minimum, err := visionChallengeCount(bytes.NewReader([]byte{0}))
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := visionChallengeCount(bytes.NewReader([]byte{4}))
	if err != nil {
		t.Fatal(err)
	}
	if minimum != 4 || maximum != 8 {
		t.Fatalf("unexpected challenge range: minimum=%d maximum=%d", minimum, maximum)
	}
}

func TestParseVisionCountAcceptsUnambiguousAnswers(t *testing.T) {
	for _, output := range []string{`4`, `4.`, `The answer is 4.`, `答案是 4。`, `{"count":4}`, "```json\n{\"count\": 4}\n```", "```\n4\n```"} {
		answer, err := parseVisionCount(output)
		if err != nil || answer != 4 {
			t.Fatalf("parseVisionCount(%q) = %d, %v; want 4", output, answer, err)
		}
	}
}

func TestParseVisionCountRejectsAmbiguousAnswers(t *testing.T) {
	for _, output := range []string{"", "four", "-4", "4.5", "4 or 5", `{"minimum":4,"maximum":5}`, `{"count":4,"guess":true}`, "As GPT-4, I cannot inspect the image.", "I cannot inspect images; I'll guess 4.", "error E4", "model8"} {
		if answer, err := parseVisionCount(output); err == nil {
			t.Fatalf("parseVisionCount(%q) = %d; want error", output, answer)
		}
	}
}
