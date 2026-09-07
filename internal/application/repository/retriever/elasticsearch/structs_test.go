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

func TestKeywordDocumentUsesExplicitGeneratedQuestionFlag(t *testing.T) {
	derivedSource := ToDBVectorEmbedding(&types.IndexInfo{
		SourceID: "derived-source",
		ChunkID:  "chunk-1",
	}, nil)
	assert.False(t, derivedSource.IsGeneratedQuestion)

	generatedQuestion := ToDBVectorEmbedding(&types.IndexInfo{
		SourceID:            "chunk-1-chunk-1-q0",
		ChunkID:             "chunk-1",
		IsGeneratedQuestion: true,
	}, nil)
	assert.True(t, generatedQuestion.IsGeneratedQuestion)
}
