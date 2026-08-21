package handler

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidateProfileSwitchAcceptsMatchingEmbeddingDimensions(t *testing.T) {
	models := []*types.Model{
		embeddingModel(types.ModelProfileOnline, 2560),
		embeddingModel(types.ModelProfileOffline, 2560),
	}
	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchAcceptsDifferentEmbeddingDimensions(t *testing.T) {
	models := []*types.Model{
		embeddingModel(types.ModelProfileOnline, 1024),
		embeddingModel(types.ModelProfileOffline, 2560),
	}
	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchAcceptsMatchingEmbeddingCompatibilityIDs(t *testing.T) {
	models := []*types.Model{
		embeddingModelWithCompatibilityID(types.ModelProfileOnline, 2560, "qwen3-embedding-4b-v1"),
		embeddingModelWithCompatibilityID(types.ModelProfileOffline, 2560, "qwen3-embedding-4b-v1"),
	}
	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchAcceptsDifferentEmbeddingCompatibilityIDs(t *testing.T) {
	models := []*types.Model{
		embeddingModelWithCompatibilityID(types.ModelProfileOnline, 2560, "qwen3-embedding-4b-v1"),
		embeddingModelWithCompatibilityID(types.ModelProfileOffline, 2560, "other-embedding-space"),
	}
	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchAcceptsPartiallyConfiguredEmbeddingCompatibilityID(t *testing.T) {
	models := []*types.Model{
		embeddingModelWithCompatibilityID(types.ModelProfileOnline, 2560, "qwen3-embedding-4b-v1"),
		embeddingModel(types.ModelProfileOffline, 2560),
	}
	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchRejectsMissingTargetEmbedding(t *testing.T) {
	err := validateProfileSwitch(
		[]*types.Model{embeddingModel(types.ModelProfileOnline, 2560)},
		types.ModelProfileOnline,
		types.ModelProfileOffline,
	)
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("error = %v, want missing target embedding", err)
	}
}

func TestValidateProfileSwitchUsesActiveDefaultEmbeddingModels(t *testing.T) {
	onlineDefault := embeddingModel(types.ModelProfileOnline, 1024)
	onlineDefault.ID = "online-default"
	onlineDefault.IsDefault = true
	offlineDefault := embeddingModel(types.ModelProfileOffline, 1024)
	offlineDefault.ID = "offline-default"
	offlineDefault.IsDefault = true
	models := []*types.Model{
		onlineDefault,
		embeddingModel(types.ModelProfileOnline, 2560),
		offlineDefault,
		embeddingModel(types.ModelProfileOffline, 2560),
	}

	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProfileSwitchIgnoresInactiveEmbeddingModels(t *testing.T) {
	inactive := embeddingModel(types.ModelProfileOffline, 2560)
	inactive.Status = types.ModelStatusDownloadFailed
	models := []*types.Model{
		embeddingModel(types.ModelProfileOnline, 1024),
		embeddingModel(types.ModelProfileOffline, 1024),
		inactive,
	}

	if err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline); err != nil {
		t.Fatal(err)
	}
}

func embeddingModel(profile types.ModelProfile, dimension int) *types.Model {
	return embeddingModelWithCompatibilityID(profile, dimension, "")
}

func embeddingModelWithCompatibilityID(profile types.ModelProfile, dimension int, compatibilityID string) *types.Model {
	return &types.Model{
		ID:          string(profile),
		Profile:     profile,
		ProfileRole: "embedding",
		Status:      types.ModelStatusActive,
		Parameters: types.ModelParameters{
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:       dimension,
				CompatibilityID: compatibilityID,
			},
		},
	}
}
