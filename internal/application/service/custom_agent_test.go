package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestValidatePlatformAgentConfigRejectsTenantSpecificKnowledgeBases(t *testing.T) {
	tests := []struct {
		name    string
		config  types.CustomAgentConfig
		wantErr bool
	}{
		{name: "all tenant knowledge bases", config: types.CustomAgentConfig{KBSelectionMode: "all"}},
		{name: "knowledge base disabled", config: types.CustomAgentConfig{KBSelectionMode: "none"}},
		{name: "selected mode", config: types.CustomAgentConfig{KBSelectionMode: "selected"}, wantErr: true},
		{name: "specific IDs", config: types.CustomAgentConfig{KBSelectionMode: "all", KnowledgeBases: []string{"tenant-kb"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlatformAgentConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePlatformAgentConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
