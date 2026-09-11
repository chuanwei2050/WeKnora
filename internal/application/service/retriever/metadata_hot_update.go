package retriever

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// chunkMetadataUpdater is the subset needed to apply whitelist patches.
type chunkMetadataUpdater interface {
	BatchUpdateChunkEnabledStatus(ctx context.Context, chunkStatusMap map[string]bool) error
	BatchUpdateChunkTagID(ctx context.Context, chunkTagMap map[string]string) error
}

// ApplyChunkMetadataPatches validates whitelist patches and delegates to existing
// enable/tag hot-update paths (no re-embed).
func ApplyChunkMetadataPatches(ctx context.Context, updater chunkMetadataUpdater, updates map[string]types.ChunkMetadataPatch) error {
	if len(updates) == 0 {
		return nil
	}
	enabled := make(map[string]bool)
	tags := make(map[string]string)
	for chunkID, patch := range updates {
		if err := patch.Validate(); err != nil {
			return fmt.Errorf("chunk %s: %w", chunkID, err)
		}
		if patch.IsEnabled != nil {
			enabled[chunkID] = *patch.IsEnabled
		}
		if patch.TagID != nil {
			tags[chunkID] = *patch.TagID
		}
	}
	if len(enabled) > 0 {
		if err := updater.BatchUpdateChunkEnabledStatus(ctx, enabled); err != nil {
			return err
		}
	}
	if len(tags) > 0 {
		if err := updater.BatchUpdateChunkTagID(ctx, tags); err != nil {
			return err
		}
	}
	return nil
}

var _ interfaces.RetrieveEngineService = (*KeywordsVectorHybridRetrieveEngineService)(nil)
