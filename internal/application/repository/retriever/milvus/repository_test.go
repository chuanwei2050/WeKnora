package milvus

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/milvus-io/milvus/client/v2/column"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportVectorOnly(t *testing.T) {
	repository := &milvusRepository{}
	assert.Equal(t, []types.RetrieverType{types.VectorRetrieverType}, repository.Support())
}

func TestConvertResultSetPreservesFloatVectors(t *testing.T) {
	want := []float32{1, 2, 3}
	resultSet := milvusclient.ResultSet{
		ResultCount: 1,
		Fields: milvusclient.DataSet{
			column.NewColumnFloatVector(fieldEmbedding, len(want), [][]float32{want}),
		},
	}

	got, _, err := convertResultSet([]milvusclient.ResultSet{resultSet})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, want, got[0].Embedding)
}
