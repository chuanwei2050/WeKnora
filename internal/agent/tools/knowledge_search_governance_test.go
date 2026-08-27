package tools

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type agentGovernanceKnowledgeService struct {
	interfaces.KnowledgeService
	items     []*types.Knowledge
	tenantIDs *[]uint64
}

func (s agentGovernanceKnowledgeService) GetKnowledgeBatchWithSharedAccess(_ context.Context, tenantID uint64, _ []string) ([]*types.Knowledge, error) {
	if s.tenantIDs != nil {
		*s.tenantIDs = append(*s.tenantIDs, tenantID)
	}
	return s.items, nil
}

type agentGovernanceRepo struct {
	interfaces.KnowledgeGovernanceRepository
	versions  map[string]*types.KnowledgeVersion
	tenantIDs *[]uint64
}

func (r agentGovernanceRepo) GetVersion(_ context.Context, tenantID uint64, _ string) (*types.KnowledgeVersion, error) {
	if r.tenantIDs != nil {
		*r.tenantIDs = append(*r.tenantIDs, tenantID)
	}
	for _, version := range r.versions {
		return version, nil
	}
	return nil, nil
}

func TestAgentKnowledgeSearchFiltersNonCurrentVersion(t *testing.T) {
	version := &types.KnowledgeVersion{ID: "current", Status: types.KnowledgeVersionActive}
	accessTenantIDs := make([]uint64, 0, 1)
	versionTenantIDs := make([]uint64, 0, 1)
	tool := &KnowledgeSearchTool{
		knowledgeService: agentGovernanceKnowledgeService{tenantIDs: &accessTenantIDs, items: []*types.Knowledge{{ID: "knowledge", TenantID: 99, KnowledgeBaseID: "kb", CurrentVersionID: version.ID}}},
		governanceRepo: agentGovernanceRepo{
			versions: map[string]*types.KnowledgeVersion{version.ID: version}, tenantIDs: &versionTenantIDs,
		},
		searchTargets: types.SearchTargets{{KnowledgeBaseID: "kb", TenantID: 9}},
	}
	results := []*searchResultWithMeta{
		{SearchResult: &types.SearchResult{ID: "old", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", KnowledgeVersionID: "old"}, KnowledgeBaseID: "kb"},
		{SearchResult: &types.SearchResult{ID: "current", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", KnowledgeVersionID: version.ID}, KnowledgeBaseID: "kb"},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	filtered := tool.filterGovernedResults(ctx, results)
	if len(filtered) != 1 || filtered[0].ID != "current" || len(accessTenantIDs) != 1 || accessTenantIDs[0] != 7 || len(versionTenantIDs) != 1 || versionTenantIDs[0] != 99 {
		t.Fatalf("unexpected governed agent results: filtered=%+v access=%v source=%v", filtered, accessTenantIDs, versionTenantIDs)
	}
}

func TestAgentKnowledgeSearchRejectsRevokedSharedKnowledge(t *testing.T) {
	accessTenantIDs := make([]uint64, 0, 1)
	tool := &KnowledgeSearchTool{
		knowledgeService: agentGovernanceKnowledgeService{tenantIDs: &accessTenantIDs},
		searchTargets:    types.SearchTargets{{KnowledgeBaseID: "shared-kb", TenantID: 99}},
	}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	filtered := tool.filterGovernedResults(ctx, []*searchResultWithMeta{{
		SearchResult:    &types.SearchResult{ID: "chunk", KnowledgeID: "knowledge", KnowledgeBaseID: "shared-kb"},
		KnowledgeBaseID: "shared-kb",
	}})
	if len(filtered) != 0 || len(accessTenantIDs) != 1 || accessTenantIDs[0] != 7 {
		t.Fatalf("revoked shared knowledge was not rejected with requester tenant: filtered=%+v access=%v", filtered, accessTenantIDs)
	}
}
