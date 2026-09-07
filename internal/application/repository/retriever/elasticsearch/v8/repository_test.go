package v8

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSupportKeywordsOnly(t *testing.T) {
	repository := &elasticsearchRepository{}
	assert.Equal(t, []types.RetrieverType{types.KeywordsRetrieverType}, repository.Support())
}

func TestKeywordIndexMappingUsesIKChineseAnalyzer(t *testing.T) {
	payload, err := json.Marshal(keywordIndexMapping())
	assert.NoError(t, err)

	assert.Contains(t, string(payload), `"content":{"analyzer":"ik_max_word","search_analyzer":"ik_smart","type":"text"}`)
}

func TestKeywordSearchCollapsesDuplicateChunkIDs(t *testing.T) {
	repository := &elasticsearchRepository{useKeywordSuffix: true}
	topK := 50

	request := repository.keywordSearchRequest(types.RetrieveParams{
		Query: "系统集成项目管理工程师",
		TopK:  topK,
	})

	assert.NotNil(t, request.Collapse)
	assert.Equal(t, "chunk_id.keyword", request.Collapse.Field)
	payload, err := json.Marshal(request)
	assert.NoError(t, err)
	assert.Contains(t, string(payload), `"analyzer":"ik_max_word"`)
}
