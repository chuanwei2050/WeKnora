package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleSpecificDefaultRetrieverDrivers(t *testing.T) {
	t.Setenv("RETRIEVE_DRIVER", "elasticsearch_v8,milvus")
	t.Setenv("KEYWORD_RETRIEVE_DRIVER", "elasticsearch_v8")
	t.Setenv("VECTOR_RETRIEVE_DRIVER", "milvus")

	assert.Equal(t, []string{"elasticsearch_v8"}, GetDefaultRetrieverDrivers(KeywordsRetrieverType))
	assert.Equal(t, []string{"milvus"}, GetDefaultRetrieverDrivers(VectorRetrieverType))
	assert.Equal(t, []RetrieverEngineParams{
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: ElasticsearchRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: MilvusRetrieverEngineType},
	}, GetDefaultRetrieverEngines())
}

func TestRoleSpecificDriverMustBeInitialized(t *testing.T) {
	t.Setenv("RETRIEVE_DRIVER", "elasticsearch_v8")
	t.Setenv("VECTOR_RETRIEVE_DRIVER", "milvus")

	assert.Empty(t, GetDefaultRetrieverDrivers(VectorRetrieverType))
}

func TestRoleSpecificDriversConstrainTenantOverrides(t *testing.T) {
	t.Setenv("KEYWORD_RETRIEVE_DRIVER", "elasticsearch_v8")
	t.Setenv("VECTOR_RETRIEVE_DRIVER", "milvus")

	tenant := &Tenant{RetrieverEngines: RetrieverEngines{Engines: []RetrieverEngineParams{
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: MilvusRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: ElasticsearchRetrieverEngineType},
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: ElasticsearchRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: MilvusRetrieverEngineType},
	}}}

	assert.Equal(t, []RetrieverEngineParams{
		{RetrieverType: KeywordsRetrieverType, RetrieverEngineType: ElasticsearchRetrieverEngineType},
		{RetrieverType: VectorRetrieverType, RetrieverEngineType: MilvusRetrieverEngineType},
	}, tenant.GetEffectiveEngines())
}
