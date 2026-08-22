package types

import "testing"

func TestNormalizeLegacyLocalModel(t *testing.T) {
	model := &Model{Source: ModelSourceLocal, Name: "qwen"}
	if err := NormalizeLegacyModelDeployment(model, true); err != nil {
		t.Fatal(err)
	}
	if model.Parameters.Protocol != ModelProtocolOllama || model.Parameters.Location != EndpointSameHost || model.Parameters.ArtifactPolicy != ArtifactPreloadedOnly {
		t.Fatalf("legacy model was not normalized: %+v", model.Parameters)
	}
}
