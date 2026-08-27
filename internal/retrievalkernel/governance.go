package retrievalkernel

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type GovernanceCandidate struct {
	Result                  *types.SearchResult
	AccessTenantID          uint64
	ExpectedKnowledgeBaseID string
}

type KnowledgeLoader func(context.Context, uint64, []string) ([]*types.Knowledge, error)
type VersionLoader func(context.Context, uint64, string) (*types.KnowledgeVersion, error)

type GovernanceResult struct {
	Accepted []bool
	Rejected map[string]int
}

// FilterGoverned applies the shared current-version and retrievability rules.
// It fails closed when metadata required for a governed document is unavailable.
func FilterGoverned(
	ctx context.Context,
	candidates []GovernanceCandidate,
	loadKnowledge KnowledgeLoader,
	loadVersion VersionLoader,
	now time.Time,
) GovernanceResult {
	result := GovernanceResult{Accepted: make([]bool, len(candidates)), Rejected: make(map[string]int)}
	if len(candidates) == 0 {
		return result
	}
	if loadKnowledge == nil {
		result.Rejected["governance_unavailable"] = len(candidates)
		return result
	}

	idsByTenant := make(map[uint64][]string)
	seenByTenant := make(map[uint64]map[string]struct{})
	for _, candidate := range candidates {
		if candidate.Result == nil || candidate.Result.ID == "" || candidate.Result.KnowledgeID == "" || candidate.ExpectedKnowledgeBaseID == "" {
			continue
		}
		if seenByTenant[candidate.AccessTenantID] == nil {
			seenByTenant[candidate.AccessTenantID] = make(map[string]struct{})
		}
		if _, exists := seenByTenant[candidate.AccessTenantID][candidate.Result.KnowledgeID]; !exists {
			seenByTenant[candidate.AccessTenantID][candidate.Result.KnowledgeID] = struct{}{}
			idsByTenant[candidate.AccessTenantID] = append(idsByTenant[candidate.AccessTenantID], candidate.Result.KnowledgeID)
		}
	}

	knowledgeByTenant := make(map[uint64]map[string]*types.Knowledge)
	for tenantID, ids := range idsByTenant {
		items, err := loadKnowledge(ctx, tenantID, ids)
		if err != nil {
			result.Rejected["metadata_lookup_failed"] += len(candidates)
			return result
		}
		knowledgeByTenant[tenantID] = make(map[string]*types.Knowledge, len(items))
		for _, knowledge := range items {
			if knowledge != nil {
				knowledgeByTenant[tenantID][knowledge.ID] = knowledge
			}
		}
	}

	for index, candidate := range candidates {
		searchResult := candidate.Result
		if searchResult == nil || searchResult.ID == "" || searchResult.KnowledgeID == "" || candidate.ExpectedKnowledgeBaseID == "" {
			result.Rejected["invalid_result"]++
			continue
		}
		knowledge := knowledgeByTenant[candidate.AccessTenantID][searchResult.KnowledgeID]
		if knowledge == nil {
			result.Rejected["metadata_missing"]++
			continue
		}
		if knowledge.KnowledgeBaseID != candidate.ExpectedKnowledgeBaseID {
			result.Rejected["knowledge_base_mismatch"]++
			continue
		}
		if knowledge.CurrentVersionID == "" {
			result.Accepted[index] = true
			continue
		}
		if searchResult.KnowledgeVersionID != knowledge.CurrentVersionID {
			result.Rejected["version_mismatch"]++
			continue
		}
		if loadVersion == nil {
			result.Rejected["governance_unavailable"]++
			continue
		}
		versionTenantID := knowledge.TenantID
		if versionTenantID == 0 {
			versionTenantID = candidate.AccessTenantID
		}
		version, err := loadVersion(ctx, versionTenantID, knowledge.CurrentVersionID)
		if err != nil || version == nil {
			result.Rejected["version_metadata_missing"]++
			continue
		}
		if !version.IsRetrievable(now) {
			if version.ExpiresAt != nil && !now.Before(*version.ExpiresAt) {
				result.Rejected["expired"]++
			} else {
				result.Rejected["not_retrievable"]++
			}
			continue
		}
		searchResult.KnowledgeLayer = version.SourceMetadata.Layer
		searchResult.SourceCategory = version.SourceMetadata.SourceCategory
		searchResult.EffectiveAt = version.EffectiveAt
		result.Accepted[index] = true
	}
	return result
}
