package types

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCustomAgentDefaultsPreserveExplicitVerificationValues(t *testing.T) {
	var agent CustomAgent
	if err := json.Unmarshal([]byte(`{
		"config": {
			"reflection_enabled": true,
			"verified_answer": {"enabled": false, "max_reflections": 0}
		}
	}`), &agent); err != nil {
		t.Fatal(err)
	}

	agent.EnsureDefaults()
	if agent.Config.VerifiedAnswer.Enabled {
		t.Fatal("explicit verified_answer.enabled=false was overwritten by the legacy reflection flag")
	}
	if agent.Config.VerifiedAnswer.MaxReflections != 0 {
		t.Fatalf("explicit max_reflections=0 was overwritten: %d", agent.Config.VerifiedAnswer.MaxReflections)
	}
}

func TestCustomAgentAGUIDefaultsToFalseAndPreservesEnabled(t *testing.T) {
	var missing CustomAgent
	if err := json.Unmarshal([]byte(`{"config":{}}`), &missing); err != nil {
		t.Fatal(err)
	}
	missing.EnsureDefaults()
	if missing.Config.AGUIEnabled {
		t.Fatal("missing agui_enabled must default to false")
	}

	var enabled CustomAgent
	if err := json.Unmarshal([]byte(`{"config":{"agui_enabled":true}}`), &enabled); err != nil {
		t.Fatal(err)
	}
	enabled.EnsureDefaults()
	if !enabled.Config.AGUIEnabled {
		t.Fatal("explicit agui_enabled=true must be preserved")
	}
}

func TestYAMLDefaultsPreserveExplicitZeroValues(t *testing.T) {
	var agent CustomAgent
	if err := yaml.Unmarshal([]byte(`config:
  keyword_threshold: 0
  vector_threshold: 0
  complexity_routing:
    confidence_threshold: 0
  reflection_enabled: true
  verified_answer:
    enabled: false
    max_reflections: 0
`), &agent); err != nil {
		t.Fatal(err)
	}

	agent.EnsureDefaults()
	if agent.Config.KeywordThreshold != 0 || agent.Config.VectorThreshold != 0 {
		t.Fatalf("explicit YAML retrieval thresholds were overwritten: keyword=%v vector=%v", agent.Config.KeywordThreshold, agent.Config.VectorThreshold)
	}
	if agent.Config.ComplexityRouting.ConfidenceThreshold != 0 {
		t.Fatalf("explicit YAML confidence threshold was overwritten: %v", agent.Config.ComplexityRouting.ConfidenceThreshold)
	}
	if agent.Config.VerifiedAnswer.Enabled || agent.Config.VerifiedAnswer.MaxReflections != 0 {
		t.Fatalf("explicit YAML verification values were overwritten: enabled=%v max_reflections=%d", agent.Config.VerifiedAnswer.Enabled, agent.Config.VerifiedAnswer.MaxReflections)
	}
}

func TestModelParametersTrackThinkingPresence(t *testing.T) {
	var legacy ModelParameters
	if err := json.Unmarshal([]byte(`{}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.HasThinkingSetting() {
		t.Fatal("missing thinking field was marked as explicitly configured")
	}

	var configured ModelParameters
	if err := json.Unmarshal([]byte(`{"thinking":false}`), &configured); err != nil {
		t.Fatal(err)
	}
	if !configured.HasThinkingSetting() {
		t.Fatal("explicit thinking=false was not tracked")
	}
}

func TestCustomAgentDefaultsStillMigrateMissingVerificationValues(t *testing.T) {
	var agent CustomAgent
	if err := json.Unmarshal([]byte(`{"config":{"reflection_enabled":true,"verified_answer":{}}}`), &agent); err != nil {
		t.Fatal(err)
	}

	agent.EnsureDefaults()
	if !agent.Config.VerifiedAnswer.Enabled {
		t.Fatal("missing verified_answer.enabled did not inherit the legacy reflection flag")
	}
	if agent.Config.VerifiedAnswer.MaxReflections != 1 {
		t.Fatalf("missing max_reflections did not receive its default: %d", agent.Config.VerifiedAnswer.MaxReflections)
	}
}

func TestComplexityRoutingDefaultsPreserveExplicitZeroThreshold(t *testing.T) {
	var config ComplexityRoutingConfig
	if err := json.Unmarshal([]byte(`{"confidence_threshold":0}`), &config); err != nil {
		t.Fatal(err)
	}
	config.EnsureDefaults()
	if config.ConfidenceThreshold != 0 {
		t.Fatalf("explicit confidence_threshold=0 was overwritten: %v", config.ConfidenceThreshold)
	}

	var missing ComplexityRoutingConfig
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
		t.Fatal(err)
	}
	missing.EnsureDefaults()
	if missing.ConfidenceThreshold != DefaultComplexityRoutingConfig().ConfidenceThreshold {
		t.Fatalf("missing confidence threshold did not receive its default: %v", missing.ConfidenceThreshold)
	}
}

func TestCustomAgentDefaultsPreserveExplicitZeroRetrievalThresholds(t *testing.T) {
	var agent CustomAgent
	if err := json.Unmarshal([]byte(`{"config":{"keyword_threshold":0,"vector_threshold":0}}`), &agent); err != nil {
		t.Fatal(err)
	}
	agent.EnsureDefaults()
	if agent.Config.KeywordThreshold != 0 || agent.Config.VectorThreshold != 0 {
		t.Fatalf("explicit zero retrieval thresholds were overwritten: keyword=%v vector=%v", agent.Config.KeywordThreshold, agent.Config.VectorThreshold)
	}

	var missing CustomAgent
	if err := json.Unmarshal([]byte(`{"config":{}}`), &missing); err != nil {
		t.Fatal(err)
	}
	missing.EnsureDefaults()
	if missing.Config.KeywordThreshold != 0.3 || missing.Config.VectorThreshold != 0.5 {
		t.Fatalf("missing retrieval thresholds did not receive defaults: keyword=%v vector=%v", missing.Config.KeywordThreshold, missing.Config.VectorThreshold)
	}
}
