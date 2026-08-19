package elasticsearch

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeywordDocumentOmitsEmbedding(t *testing.T) {
	document := ToDBVectorEmbedding(&types.IndexInfo{
		SourceID: "source-1",
		ChunkID:  "chunk-1",
		Content:  "keyword content",
	}, map[string]any{"embedding": map[string][]float32{"source-1": {0.1, 0.2}}})

	data, err := json.Marshal(document)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"embedding"`)
}
