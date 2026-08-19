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

func TestValidateProfileSwitchRejectsDifferentEmbeddingDimensions(t *testing.T) {
	models := []*types.Model{
		embeddingModel(types.ModelProfileOnline, 1024),
		embeddingModel(types.ModelProfileOffline, 2560),
	}
	err := validateProfileSwitch(models, types.ModelProfileOnline, types.ModelProfileOffline)
	if err == nil || !strings.Contains(err.Error(), "1024") || !strings.Contains(err.Error(), "2560") {
		t.Fatalf("error = %v, want dimension mismatch", err)
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
	return &types.Model{
		ID:          string(profile),
		Profile:     profile,
		ProfileRole: "embedding",
		Status:      types.ModelStatusActive,
		Parameters: types.ModelParameters{
			EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension},
		},
	}
}
