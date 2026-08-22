package v8

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSupportKeywordsOnly(t *testing.T) {
	repository := &elasticsearchRepository{}
	assert.Equal(t, []types.RetrieverType{types.KeywordsRetrieverType}, repository.Support())
}
