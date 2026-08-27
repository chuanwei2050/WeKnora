package chatpipeline

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type governanceKnowledgeFixture struct {
	interfaces.KnowledgeService
	items     []*types.Knowledge
	tenantIDs *[]uint64
}

func (f governanceKnowledgeFixture) GetKnowledgeBatchWithSharedAccess(_ context.Context, tenantID uint64, _ []string) ([]*types.Knowledge, error) {
	if f.tenantIDs != nil {
		*f.tenantIDs = append(*f.tenantIDs, tenantID)
	}
	return f.items, nil
}

type governanceVersionFixture struct {
	interfaces.KnowledgeGovernanceRepository
	versions  map[string]*types.KnowledgeVersion
	tenantIDs *[]uint64
}

func (f governanceVersionFixture) GetVersion(_ context.Context, tenantID uint64, id string) (*types.KnowledgeVersion, error) {
	if f.tenantIDs != nil {
		*f.tenantIDs = append(*f.tenantIDs, tenantID)
	}
	return f.versions[id], nil
}

func TestFilterGovernedSearchResultsKeepsOnlyCurrentRetrievableVersion(t *testing.T) {
	now := time.Now().UTC()
	current := &types.KnowledgeVersion{
		ID: "version-current", Status: types.KnowledgeVersionActive,
		SourceMetadata: types.KnowledgeSourceMetadata{Layer: types.KnowledgeLayerFoundation, SourceCategory: "official", AuthorityLevel: "primary"},
	}
	futureAt := now.Add(time.Hour)
	future := &types.KnowledgeVersion{ID: "version-future", Status: types.KnowledgeVersionActive, EffectiveAt: &futureAt}
	expiredAt := now.Add(-time.Hour)
	expired := &types.KnowledgeVersion{ID: "version-expired", Status: types.KnowledgeVersionActive, ExpiresAt: &expiredAt}

	knowledge := governanceKnowledgeFixture{items: []*types.Knowledge{
		{ID: "knowledge-current", KnowledgeBaseID: "kb", CurrentVersionID: current.ID},
		{ID: "knowledge-future", KnowledgeBaseID: "kb", CurrentVersionID: future.ID},
		{ID: "knowledge-expired", KnowledgeBaseID: "kb", CurrentVersionID: expired.ID},
		{ID: "knowledge-legacy", KnowledgeBaseID: "kb"},
	}}
	plugin := &PluginSearch{
		knowledgeService: knowledge,
		governanceRepo: governanceVersionFixture{versions: map[string]*types.KnowledgeVersion{
			current.ID: current, future.ID: future, expired.ID: expired,
		}},
	}
	results := []*types.SearchResult{
		{ID: "chunk-current", KnowledgeID: "knowledge-current", KnowledgeBaseID: "kb", KnowledgeVersionID: current.ID},
		{ID: "chunk-old", KnowledgeID: "knowledge-current", KnowledgeBaseID: "kb", KnowledgeVersionID: "version-old"},
		{ID: "chunk-future", KnowledgeID: "knowledge-future", KnowledgeBaseID: "kb", KnowledgeVersionID: future.ID},
		{ID: "chunk-expired", KnowledgeID: "knowledge-expired", KnowledgeBaseID: "kb", KnowledgeVersionID: expired.ID},
		{ID: "chunk-legacy", KnowledgeID: "knowledge-legacy", KnowledgeBaseID: "kb"},
		{ID: "chunk-missing", KnowledgeID: "knowledge-missing", KnowledgeBaseID: "kb"},
	}

	filtered := plugin.filterGovernedSearchResults(context.Background(), 7, types.SearchTargets{{KnowledgeBaseID: "kb"}}, results)
	if len(filtered) != 2 || filtered[0].ID != "chunk-current" || filtered[1].ID != "chunk-legacy" {
		t.Fatalf("unexpected governed visibility result: %+v", filtered)
	}
	if filtered[0].KnowledgeLayer != types.KnowledgeLayerFoundation || filtered[0].SourceCategory != "official" {
		t.Fatalf("current version metadata was not propagated: %+v", filtered[0])
	}
}

func TestFilterGovernedSearchResultsUsesSharedKnowledgeTenantForVersion(t *testing.T) {
	version := &types.KnowledgeVersion{ID: "shared-version", Status: types.KnowledgeVersionActive}
	tenantIDs := make([]uint64, 0, 1)
	accessTenantIDs := make([]uint64, 0, 1)
	plugin := &PluginSearch{
		knowledgeService: governanceKnowledgeFixture{tenantIDs: &accessTenantIDs, items: []*types.Knowledge{
			{ID: "shared-knowledge", TenantID: 99, KnowledgeBaseID: "kb", CurrentVersionID: version.ID},
		}},
		governanceRepo: governanceVersionFixture{
			versions:  map[string]*types.KnowledgeVersion{version.ID: version},
			tenantIDs: &tenantIDs,
		},
	}

	filtered := plugin.filterGovernedSearchResults(context.Background(), 7, types.SearchTargets{{KnowledgeBaseID: "kb"}}, []*types.SearchResult{
		{ID: "shared-chunk", KnowledgeID: "shared-knowledge", KnowledgeBaseID: "kb", KnowledgeVersionID: version.ID},
	})
	if len(filtered) != 1 || len(accessTenantIDs) != 1 || accessTenantIDs[0] != 7 || len(tenantIDs) != 1 || tenantIDs[0] != 99 {
		t.Fatalf("shared governance tenant boundary mismatch: filtered=%+v access=%v source=%v", filtered, accessTenantIDs, tenantIDs)
	}
}

func TestFilterGovernedSearchResultsRejectsRevokedSharedKnowledge(t *testing.T) {
	accessTenantIDs := make([]uint64, 0, 1)
	plugin := &PluginSearch{knowledgeService: governanceKnowledgeFixture{tenantIDs: &accessTenantIDs}}
	filtered := plugin.filterGovernedSearchResults(
		context.Background(), 7, types.SearchTargets{{KnowledgeBaseID: "shared-kb", TenantID: 99}},
		[]*types.SearchResult{{ID: "chunk", KnowledgeID: "knowledge", KnowledgeBaseID: "shared-kb"}},
	)
	if len(filtered) != 0 || len(accessTenantIDs) != 1 || accessTenantIDs[0] != 7 {
		t.Fatalf("revoked shared knowledge was not rejected with requester tenant: filtered=%+v access=%v", filtered, accessTenantIDs)
	}
}

func TestGovernanceRunsBeforeCandidateLimit(t *testing.T) {
	version := &types.KnowledgeVersion{ID: "current", Status: types.KnowledgeVersionActive}
	plugin := &PluginSearch{
		knowledgeService: governanceKnowledgeFixture{items: []*types.Knowledge{{ID: "knowledge", KnowledgeBaseID: "kb", CurrentVersionID: version.ID}}},
		governanceRepo:   governanceVersionFixture{versions: map[string]*types.KnowledgeVersion{version.ID: version}},
	}
	manage := &types.ChatManage{PipelineRequest: types.PipelineRequest{
		TenantID: 7, EmbeddingTopK: 1, SearchTargets: types.SearchTargets{{KnowledgeBaseID: "kb"}},
	}}
	results := []*types.SearchResult{
		{ID: "stale-high", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", KnowledgeVersionID: "stale", Score: 0.99},
		{ID: "current-low", KnowledgeID: "knowledge", KnowledgeBaseID: "kb", KnowledgeVersionID: version.ID, Score: 0.2},
	}
	filtered := plugin.governAndLimitResults(context.Background(), manage, results)
	if len(filtered) != 1 || filtered[0].ID != "current-low" {
		t.Fatalf("invalid version evicted the valid candidate before governance: %+v", filtered)
	}
}
