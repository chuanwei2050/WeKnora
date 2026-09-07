package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type KnowledgeTableSchemaRepository interface {
	Upsert(ctx context.Context, knowledge *types.Knowledge, schemaJSON []byte) error
	GetCurrent(ctx context.Context, knowledge *types.Knowledge) ([]byte, bool, error)
	DeleteKnowledge(ctx context.Context, tenantID uint64, knowledgeID string) error
	ListMissing(ctx context.Context, tenantID uint64, limit int) ([]*types.Knowledge, error)
}
