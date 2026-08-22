package milvus

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSupportVectorOnly(t *testing.T) {
	repository := &milvusRepository{}
	assert.Equal(t, []types.RetrieverType{types.VectorRetrieverType}, repository.Support())
}
