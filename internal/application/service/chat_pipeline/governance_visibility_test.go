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
	items []*types.Knowledge
}

func (f governanceKnowledgeFixture) GetKnowledgeBatch(context.Context, uint64, []string) ([]*types.Knowledge, error) {
	return f.items, nil
}

type governanceVersionFixture struct {
	interfaces.KnowledgeGovernanceRepository
	versions map[string]*types.KnowledgeVersion
}

func (f governanceVersionFixture) GetVersion(_ context.Context, _ uint64, id string) (*types.KnowledgeVersion, error) {
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
		{ID: "knowledge-current", CurrentVersionID: current.ID},
		{ID: "knowledge-future", CurrentVersionID: future.ID},
		{ID: "knowledge-expired", CurrentVersionID: expired.ID},
		{ID: "knowledge-legacy"},
	}}
	plugin := &PluginSearch{
		knowledgeService: knowledge,
		governanceRepo: governanceVersionFixture{versions: map[string]*types.KnowledgeVersion{
			current.ID: current, future.ID: future, expired.ID: expired,
		}},
	}
	results := []*types.SearchResult{
		{ID: "chunk-current", KnowledgeID: "knowledge-current", KnowledgeVersionID: current.ID},
		{ID: "chunk-old", KnowledgeID: "knowledge-current", KnowledgeVersionID: "version-old"},
		{ID: "chunk-future", KnowledgeID: "knowledge-future", KnowledgeVersionID: future.ID},
		{ID: "chunk-expired", KnowledgeID: "knowledge-expired", KnowledgeVersionID: expired.ID},
		{ID: "chunk-legacy", KnowledgeID: "knowledge-legacy"},
	}

	filtered := plugin.filterGovernedSearchResults(context.Background(), 7, results)
	if len(filtered) != 2 || filtered[0].ID != "chunk-current" || filtered[1].ID != "chunk-legacy" {
		t.Fatalf("unexpected governed visibility result: %+v", filtered)
	}
	if filtered[0].KnowledgeLayer != types.KnowledgeLayerFoundation || filtered[0].SourceCategory != "official" {
		t.Fatalf("current version metadata was not propagated: %+v", filtered[0])
	}
}
