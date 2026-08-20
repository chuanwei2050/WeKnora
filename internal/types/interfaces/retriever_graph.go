package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// RetrieveGraphRepository is a repository for retrieving graphs
type RetrieveGraphRepository interface {
	// AddGraph adds a graph to the repository
	AddGraph(ctx context.Context, namespace types.NameSpace, graphs []*types.GraphData) error
	// DelGraph deletes a graph from the repository
	DelGraph(ctx context.Context, namespace []types.NameSpace) error
	// SearchNode searches for nodes in the repository
	SearchNode(ctx context.Context, namespace types.NameSpace, nodes []string) (*types.GraphData, error)
	SearchPaths(ctx context.Context, query types.GraphQuery) (*types.GraphSearchResult, error)
	EnsureCanonicalSchema(ctx context.Context) error
	UpsertCanonicalRecords(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, records []types.GraphRebuildRecord) error
	ReplaceCanonicalSourceRecords(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, source types.GraphSource, records []types.GraphRebuildRecord) error
	RemoveCanonicalSource(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, source types.GraphSource) error
	DeleteCanonicalKnowledgeBase(ctx context.Context, tenantID uint64, knowledgeBaseID string) error
	RebuildCanonicalGraph(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string, records []types.GraphRebuildRecord, switchActive bool) (types.GraphRebuildResult, error)
	SwitchCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID, namespace string) error
	RollbackCanonicalNamespace(ctx context.Context, tenantID uint64, knowledgeBaseID string) (string, error)
}
