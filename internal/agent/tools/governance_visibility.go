package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type agentVersionedChunkPager interface {
	ListPagedChunksByKnowledgeVersionID(ctx context.Context, tenantID uint64, knowledgeID, versionID string, page *types.Pagination, chunkType []types.ChunkType, tagID, keyword, searchField, sortOrder, knowledgeType string) ([]*types.Chunk, int64, error)
}

func listAgentChunks(
	ctx context.Context,
	repo interfaces.ChunkRepository,
	tenantID uint64,
	knowledgeID string,
	versionID string,
	page *types.Pagination,
	chunkTypes []types.ChunkType,
) ([]*types.Chunk, int64, error) {
	if versionID != "" {
		if pager, ok := repo.(agentVersionedChunkPager); ok {
			return pager.ListPagedChunksByKnowledgeVersionID(ctx, tenantID, knowledgeID, versionID, page, chunkTypes, "", "", "", "", "")
		}
		return nil, 0, fmt.Errorf("version-filtered chunk listing is unavailable")
	}
	return repo.ListPagedChunksByKnowledgeID(ctx, tenantID, knowledgeID, page, chunkTypes, "", "", "", "", "")
}

// knowledgeInAgentSearchScope checks both the KB owner tenant and the
// optional document allow-list. A KB target authorizes every document in that
// KB; a document target authorizes only its listed IDs.
func knowledgeInAgentSearchScope(knowledge *types.Knowledge, targets types.SearchTargets) bool {
	if knowledge == nil {
		return false
	}
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID != knowledge.KnowledgeBaseID || target.TenantID != knowledge.TenantID {
			continue
		}
		if target.Type == types.SearchTargetTypeKnowledgeBase || (target.Type == types.SearchTargetTypeKnowledge && target.KnowledgeIDs == nil) {
			return true
		}
		for _, knowledgeID := range target.KnowledgeIDs {
			if knowledgeID == knowledge.ID {
				return true
			}
		}
	}
	return false
}

// governedKnowledgeRetrievable applies the same current-version and validity
// window rule used by normal RAG retrieval. Documents without governance
// pointers are legacy/ungoverned records and remain eligible.
func governedKnowledgeRetrievable(ctx context.Context, knowledge *types.Knowledge, repo interfaces.KnowledgeGovernanceRepository) bool {
	if knowledge == nil {
		return false
	}
	currentVersionID := strings.TrimSpace(knowledge.CurrentVersionID)
	if currentVersionID == "" && strings.TrimSpace(knowledge.PendingVersionID) == "" {
		if repo == nil {
			return true
		}
		// An expired governed version used to clear the current pointer. Use
		// the immutable version history to distinguish that state from a
		// genuinely legacy document instead of treating it as ungoverned.
		versions, err := repo.ListVersions(ctx, knowledge.TenantID, knowledge.ID)
		return err == nil && len(versions) == 0
	}
	if currentVersionID == "" || strings.TrimSpace(knowledge.PendingVersionID) != "" {
		return false
	}
	if repo == nil {
		return false
	}
	version, err := repo.GetVersion(ctx, knowledge.TenantID, currentVersionID)
	return err == nil && version != nil && version.IsRetrievable(time.Now().UTC())
}

func agentKnowledgeVisible(ctx context.Context, knowledge *types.Knowledge, targets types.SearchTargets, repo interfaces.KnowledgeGovernanceRepository) bool {
	// Nil targets identify legacy/internal callers that do not have an agent
	// search scope. They still receive governance filtering, while agent
	// registrations pass a non-nil scope and get tenant/document enforcement.
	if targets == nil {
		return governedKnowledgeRetrievable(ctx, knowledge, repo)
	}
	return knowledgeInAgentSearchScope(knowledge, targets) && governedKnowledgeRetrievable(ctx, knowledge, repo)
}

func filterAgentVisibleChunks(chunks []*types.Chunk, knowledge *types.Knowledge) []*types.Chunk {
	if knowledge == nil || strings.TrimSpace(knowledge.CurrentVersionID) == "" {
		return chunks
	}
	currentVersionID := strings.TrimSpace(knowledge.CurrentVersionID)
	filtered := make([]*types.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil && strings.TrimSpace(chunk.KnowledgeVersionID) == currentVersionID {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func maskPendingKnowledge(knowledge *types.Knowledge) *types.Knowledge {
	if knowledge == nil || strings.TrimSpace(knowledge.PendingVersionID) == "" {
		return knowledge
	}
	masked := *knowledge
	masked.Title = ""
	masked.Description = ""
	masked.Source = ""
	masked.FileName = ""
	masked.FileType = ""
	masked.FilePath = ""
	masked.FileSize = 0
	masked.StorageSize = 0
	masked.Metadata = nil
	return &masked
}
