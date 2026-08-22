package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyNewDocumentKnowledgeBaseDefaults(t *testing.T) {
	kb := &types.KnowledgeBase{Type: types.KnowledgeBaseTypeDocument}
	applyNewDocumentKnowledgeBaseDefaults(kb)
	if !kb.Governance.Enabled {
		t.Fatal("expected governance to be enabled by default")
	}
	if kb.Governance.ProfileID != "software-testing" || kb.Governance.ProfileVersion != "1.0" {
		t.Fatalf("unexpected governance profile: %+v", kb.Governance)
	}
	if kb.ContributionMode != types.ContributionModeMembers {
		t.Fatalf("expected members contribution mode, got %s", kb.ContributionMode)
	}
}

func TestApplyNewDocumentKnowledgeBaseDefaultsSkipsFAQ(t *testing.T) {
	kb := &types.KnowledgeBase{Type: types.KnowledgeBaseTypeFAQ}
	applyNewDocumentKnowledgeBaseDefaults(kb)
	if kb.Governance.Enabled {
		t.Fatal("faq knowledge base should not receive document governance defaults")
	}
}

func TestApplyNewDocumentKnowledgeBaseDefaultsPreservesClosedMode(t *testing.T) {
	kb := &types.KnowledgeBase{
		Type:             types.KnowledgeBaseTypeDocument,
		ContributionMode: types.ContributionModeClosed,
		Governance: types.KnowledgeGovernanceConfig{
			Enabled:        true,
			ProfileID:      "software-testing",
			ProfileVersion: "1.0",
		},
	}
	applyNewDocumentKnowledgeBaseDefaults(kb)
	if kb.ContributionMode != types.ContributionModeClosed {
		t.Fatalf("expected closed contribution mode to be preserved, got %s", kb.ContributionMode)
	}
}
