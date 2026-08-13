package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAppendKnowledgeRefsCollectsAndDeduplicatesSuccessfulSearches(t *testing.T) {
	existing := &types.SearchResult{ID: "existing"}
	newRef := &types.SearchResult{ID: "new"}
	duplicate := &types.SearchResult{ID: "existing"}

	refs := appendKnowledgeRefs([]*types.SearchResult{existing}, types.AgentStep{
		ToolCalls: []types.ToolCall{
			{
				Name: "knowledge_search",
				Result: &types.ToolResult{
					Success: true,
					Data: map[string]interface{}{
						knowledgeSearchResultsDataKey: []*types.SearchResult{duplicate, newRef},
					},
				},
			},
			{
				Name: "knowledge_search",
				Result: &types.ToolResult{
					Success: false,
					Data: map[string]interface{}{
						knowledgeSearchResultsDataKey: []*types.SearchResult{{ID: "failed"}},
					},
				},
			},
		},
	})

	require.Equal(t, []string{"existing", "new"}, []string{refs[0].ID, refs[1].ID})
}

func TestAppendKnowledgeRefsIgnoresUnstructuredToolData(t *testing.T) {
	refs := appendKnowledgeRefs(nil, types.AgentStep{ToolCalls: []types.ToolCall{{
		Name: "knowledge_search",
		Result: &types.ToolResult{Success: true, Data: map[string]interface{}{
			knowledgeSearchResultsDataKey: []interface{}{map[string]interface{}{"id": "chunk-1"}},
		}},
	}}})

	require.Empty(t, refs)
}
