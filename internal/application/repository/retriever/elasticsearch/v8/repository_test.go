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
