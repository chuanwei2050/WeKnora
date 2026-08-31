package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type agentVisibilityGovernanceRepo struct {
	interfaces.KnowledgeGovernanceRepository
	version *types.KnowledgeVersion
}

func (r agentVisibilityGovernanceRepo) GetVersion(context.Context, uint64, string) (*types.KnowledgeVersion, error) {
	return r.version, nil
}

func TestAgentKnowledgeVisibleRequiresScopedTenantAndDocument(t *testing.T) {
	targets := types.SearchTargets{{
		Type:            types.SearchTargetTypeKnowledge,
		KnowledgeBaseID: "kb-1",
		TenantID:        7,
		KnowledgeIDs:    []string{"knowledge-1"},
	}}
	knowledge := &types.Knowledge{ID: "knowledge-1", KnowledgeBaseID: "kb-1", TenantID: 7}
	if !agentKnowledgeVisible(context.Background(), knowledge, targets, nil) {
		t.Fatal("ungoverned knowledge in the authorized scope should be visible")
	}
	knowledge.TenantID = 8
	if agentKnowledgeVisible(context.Background(), knowledge, targets, nil) {
		t.Fatal("knowledge from another tenant should not be visible")
	}
}

func TestAgentKnowledgeVisibleChecksCurrentVersionValidity(t *testing.T) {
	now := time.Now().UTC()
	knowledge := &types.Knowledge{ID: "knowledge-1", KnowledgeBaseID: "kb-1", TenantID: 7, CurrentVersionID: "version-1", PendingVersionID: "version-2"}
	repo := agentVisibilityGovernanceRepo{version: &types.KnowledgeVersion{Status: types.KnowledgeVersionActive, EffectiveAt: &now}}
	targets := types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 7}}
	if agentKnowledgeVisible(context.Background(), knowledge, targets, repo) {
		t.Fatal("knowledge with a pending version should not be visible to agent retrieval")
	}
	expired := now.Add(-time.Minute)
	repo.version.ExpiresAt = &expired
	if agentKnowledgeVisible(context.Background(), knowledge, targets, repo) {
		t.Fatal("expired current version should not be visible")
	}
	knowledge.CurrentVersionID = ""
	if agentKnowledgeVisible(context.Background(), knowledge, targets, repo) {
		t.Fatal("knowledge with only a pending version should not be visible")
	}
}

func TestFilterAgentVisibleChunksKeepsOnlyCurrentVersion(t *testing.T) {
	chunks := []*types.Chunk{
		{ID: "old", KnowledgeVersionID: "version-old"},
		{ID: "current", KnowledgeVersionID: "version-current"},
	}
	visible := filterAgentVisibleChunks(chunks, &types.Knowledge{CurrentVersionID: "version-current"})
	if len(visible) != 1 || visible[0].ID != "current" {
		t.Fatalf("visible chunks = %+v", visible)
	}
}
