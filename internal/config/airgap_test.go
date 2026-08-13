package config

import (
	"strings"
	"testing"
)

func TestValidateConfigAirGappedRejectsPublicModel(t *testing.T) {
	cfg := &Config{
		AirGapped: true,
		Models: []ModelConfig{{
			Source:    "remote",
			ModelName: "public-model",
			Parameters: map[string]interface{}{
				"base_url":             "https://8.8.8.8/v1",
				"approved_endpoint_id": "public-endpoint",
			},
		}},
	}

	err := ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "resolves outside private network") {
		t.Fatalf("expected public model endpoint to be rejected, got %v", err)
	}
}

func TestValidateConfigAirGappedRejectsPublicConfiguredDependency(t *testing.T) {
	t.Setenv("S3_ENDPOINT", "https://8.8.8.8")

	err := ValidateConfig(&Config{AirGapped: true})
	if err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT") {
		t.Fatalf("expected public storage endpoint to be rejected, got %v", err)
	}
}

func TestValidateConfigAirGappedRejectsPublicTelemetryAndConnector(t *testing.T) {
	t.Run("telemetry", func(t *testing.T) {
		t.Setenv("LANGFUSE_HOST", "https://8.8.8.8")
		err := ValidateConfig(&Config{AirGapped: true})
		if err == nil || !strings.Contains(err.Error(), "LANGFUSE_HOST") {
			t.Fatalf("expected public telemetry endpoint to be rejected, got %v", err)
		}
	})
	t.Run("document reader", func(t *testing.T) {
		t.Setenv("DOCREADER_ADDR", "8.8.8.8:8080")
		err := ValidateConfig(&Config{AirGapped: true})
		if err == nil || !strings.Contains(err.Error(), "DOCREADER_ADDR") {
			t.Fatalf("expected public data connector endpoint to be rejected, got %v", err)
		}
	})
}

func TestApplyAirGapEnvOverridesEnablesStrictMode(t *testing.T) {
	t.Setenv("AIR_GAPPED_MODE", "true")
	cfg := &Config{}

	applyAirGapEnvOverrides(cfg)
	if !cfg.AirGapped {
		t.Fatal("expected AIR_GAPPED_MODE=true to enable strict mode")
	}
}
