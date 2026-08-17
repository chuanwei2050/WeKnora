package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestModelDefaultTTSVoice(t *testing.T) {
	model := &types.Model{Parameters: types.ModelParameters{ExtraConfig: map[string]string{"voice": " model:alex "}}}
	if got := modelDefaultTTSVoice(model); got != "model:alex" {
		t.Fatalf("modelDefaultTTSVoice() = %q, want %q", got, "model:alex")
	}

	model.Parameters.ExtraConfig = map[string]string{"voice_name": "alloy"}
	if got := modelDefaultTTSVoice(model); got != "alloy" {
		t.Fatalf("legacy voice_name = %q, want alloy", got)
	}
}
