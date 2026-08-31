package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type visibilityGovernanceRepo struct {
	interfaces.KnowledgeGovernanceRepository
	version *types.KnowledgeVersion
}

func (r visibilityGovernanceRepo) GetVersion(context.Context, uint64, string) (*types.KnowledgeVersion, error) {
	return r.version, nil
}

func TestCanViewGovernedKnowledgeKeepsCurrentVersionVisibleWhilePending(t *testing.T) {
	h := &KnowledgeHandler{}
	kb := &types.KnowledgeBase{Governance: types.KnowledgeGovernanceConfig{Enabled: true}}
	ctx := context.Background()

	if h.canViewGovernedKnowledge(ctx, &types.Knowledge{
		CurrentVersionID: "current",
		PendingVersionID: "pending",
	}, kb) {
		t.Fatal("ordinary reader can view a knowledge with a pending governed version")
	}
	if !h.canViewGovernedKnowledge(ctx, &types.Knowledge{CurrentVersionID: "current"}, kb) {
		t.Fatal("ordinary reader cannot view a knowledge without a pending governed version")
	}
}

func TestCanViewGovernedKnowledgeRejectsExpiredCurrentVersion(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Minute)
	h := &KnowledgeHandler{governanceRepo: visibilityGovernanceRepo{version: &types.KnowledgeVersion{
		Status:    types.KnowledgeVersionActive,
		ExpiresAt: &expired,
	}}}
	kb := &types.KnowledgeBase{Governance: types.KnowledgeGovernanceConfig{Enabled: true}}
	if h.canViewGovernedKnowledge(context.Background(), &types.Knowledge{TenantID: 1, CurrentVersionID: "current"}, kb) {
		t.Fatal("ordinary reader can view an expired current version")
	}
}
