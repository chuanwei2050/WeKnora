package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openGraphTripleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:graph_triple_%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS graph_triple_candidates (
		id TEXT PRIMARY KEY,
		tenant_id INTEGER NOT NULL,
		knowledge_base_id TEXT NOT NULL,
		knowledge_id TEXT NOT NULL,
		knowledge_version_id TEXT,
		chunk_id TEXT NOT NULL,
		model_id TEXT,
		config_fingerprint TEXT,
		graph_data TEXT NOT NULL,
		status TEXT NOT NULL,
		reviewer_id TEXT,
		comment TEXT,
		superseded_by TEXT,
		created_at DATETIME,
		reviewed_at DATETIME,
		written_at DATETIME
	)`).Error)
	return db
}

func TestGraphTripleEnqueueSupersedesPending(t *testing.T) {
	db := openGraphTripleTestDB(t)
	repo := NewGraphTripleReviewRepository(db)
	ctx := context.Background()

	first := &types.GraphTripleCandidate{
		TenantID:        1,
		KnowledgeBaseID: "kb1",
		KnowledgeID:     "k1",
		ChunkID:         "c1",
		GraphData:       types.GraphDataPayload{Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}}},
		Status:          types.GraphTriplePending,
		CreatedAt:       time.Now().UTC(),
	}
	require.NoError(t, repo.Enqueue(ctx, first))

	second := &types.GraphTripleCandidate{
		TenantID:        1,
		KnowledgeBaseID: "kb1",
		KnowledgeID:     "k1",
		ChunkID:         "c1",
		GraphData:       types.GraphDataPayload{Relation: []*types.GraphRelation{{Node1: "A", Node2: "C", Type: "tests"}}},
		Status:          types.GraphTriplePending,
		CreatedAt:       time.Now().UTC(),
	}
	require.NoError(t, repo.Enqueue(ctx, second))

	old, err := repo.GetByID(ctx, 1, first.ID)
	require.NoError(t, err)
	require.Equal(t, types.GraphTripleSuperseded, old.Status)
	require.Equal(t, second.ID, old.SupersededBy)

	require.NoError(t, repo.MarkRejected(ctx, 1, second.ID, "u1", "bad"))
	got, err := repo.GetByID(ctx, 1, second.ID)
	require.NoError(t, err)
	require.Equal(t, types.GraphTripleRejected, got.Status)
	require.Error(t, repo.MarkWritten(ctx, 1, second.ID, "u1"))
}

func TestGraphTripleMarkWrittenKeepsPendingOnSkip(t *testing.T) {
	db := openGraphTripleTestDB(t)
	repo := NewGraphTripleReviewRepository(db)
	ctx := context.Background()
	item := &types.GraphTripleCandidate{
		TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "k", ChunkID: "c",
		GraphData: types.GraphDataPayload{Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}}},
		Status:    types.GraphTriplePending, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Enqueue(ctx, item))
	require.NoError(t, repo.MarkWritten(ctx, 1, item.ID, "reviewer"))
	got, err := repo.GetByID(ctx, 1, item.ID)
	require.NoError(t, err)
	require.Equal(t, types.GraphTripleWritten, got.Status)
	require.NotNil(t, got.WrittenAt)
	require.Error(t, repo.MarkWritten(ctx, 1, item.ID, "reviewer"))
}
