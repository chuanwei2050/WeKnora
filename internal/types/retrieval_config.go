package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

const (
	DefaultEmbeddingTopK        = 30
	DefaultVectorRecallTopK     = 50
	DefaultKeywordRecallTopK    = 50
	DefaultRRFVectorWeight      = 0.7
	DefaultVectorThreshold      = 0.3
	DefaultKeywordThreshold     = 0.3
	DefaultRerankCandidateTopK  = 20
	DefaultRerankTopK           = 10
	DefaultRerankThreshold      = 0.3
	DefaultBatchMaxResults      = 200
	DefaultBatchMaxContentChars = 200000
	MaxBatchMaxResults          = 5000
	MaxBatchMaxContentChars     = 10000000
)

// RetrievalConfig holds the global retrieval/search configuration for a tenant.
// This replaces the retrieval-related fields previously scattered in ConversationConfig
// and ChatHistoryConfig. Both knowledge search and message search share these parameters.
//
// Stored as a JSONB column on the tenants table, managed via the settings UI
// at /tenants/kv/retrieval-config.
type RetrievalConfig struct {
	// EmbeddingTopK is the maximum number of chunks returned by hybrid search (default: 30)
	EmbeddingTopK int `json:"embedding_top_k"`
	// VectorRecallTopK is the total vector recall budget across all selected knowledge bases.
	VectorRecallTopK int `json:"vector_recall_top_k"`
	// KeywordRecallTopK is the total keyword recall budget across all selected knowledge bases.
	KeywordRecallTopK int `json:"keyword_recall_top_k"`
	// RRFVectorWeight controls vector contribution during reciprocal rank fusion.
	RRFVectorWeight float64 `json:"rrf_vector_weight"`
	// VectorThreshold is the minimum vector similarity score (0-1, default: 0.3)
	VectorThreshold float64 `json:"vector_threshold"`
	// KeywordThreshold is the minimum keyword match score (0-1, default: 0.3)
	KeywordThreshold float64 `json:"keyword_threshold"`
	// RerankTopK is the maximum number of results after reranking (default: 10)
	RerankTopK int `json:"rerank_top_k"`
	// RerankCandidateTopK is the maximum number of fused candidates sent to reranking.
	RerankCandidateTopK int `json:"rerank_candidate_top_k"`
	// RerankThreshold is the minimum rerank score (-10 to 10, default: 0.3)
	RerankThreshold float64 `json:"rerank_threshold"`
	// RerankModelID is the ID of the rerank model to use (required for search)
	RerankModelID string `json:"rerank_model_id"`
	// EnableQueryExpansion is the platform-wide ceiling for model-driven query expansion.
	EnableQueryExpansion bool `json:"enable_query_expansion"`
	// BatchMaxResults limits the total number of results returned by one Integration batch search.
	BatchMaxResults int `json:"batch_max_results"`
	// BatchMaxContentChars limits total Unicode characters in batch result content.
	BatchMaxContentChars int `json:"batch_max_content_chars"`

	rrfVectorWeightSet  bool
	vectorThresholdSet  bool
	keywordThresholdSet bool
	rerankThresholdSet  bool
	queryExpansionSet   bool
}

// RetrievalConfigUpdate preserves the distinction between an omitted legacy field
// and an explicitly submitted zero at the HTTP boundary.
type RetrievalConfigUpdate struct {
	EmbeddingTopK        *int     `json:"embedding_top_k"`
	VectorRecallTopK     *int     `json:"vector_recall_top_k"`
	KeywordRecallTopK    *int     `json:"keyword_recall_top_k"`
	RRFVectorWeight      *float64 `json:"rrf_vector_weight"`
	VectorThreshold      *float64 `json:"vector_threshold"`
	KeywordThreshold     *float64 `json:"keyword_threshold"`
	RerankCandidateTopK  *int     `json:"rerank_candidate_top_k"`
	RerankTopK           *int     `json:"rerank_top_k"`
	RerankThreshold      *float64 `json:"rerank_threshold"`
	EnableQueryExpansion *bool    `json:"enable_query_expansion"`
	BatchMaxResults      *int     `json:"batch_max_results"`
	BatchMaxContentChars *int     `json:"batch_max_content_chars"`
}

// ApplyRetrievalConfigUpdate merges an API update onto the normalized platform value.
func ApplyRetrievalConfigUpdate(current *RetrievalConfig, update RetrievalConfigUpdate) RetrievalConfig {
	result := NormalizeRetrievalConfig(current)
	if update.EmbeddingTopK != nil {
		result.EmbeddingTopK = *update.EmbeddingTopK
	}
	if update.VectorRecallTopK != nil {
		result.VectorRecallTopK = *update.VectorRecallTopK
	}
	if update.KeywordRecallTopK != nil {
		result.KeywordRecallTopK = *update.KeywordRecallTopK
	}
	if update.RRFVectorWeight != nil {
		result.RRFVectorWeight = *update.RRFVectorWeight
		result.rrfVectorWeightSet = true
	}
	if update.VectorThreshold != nil {
		result.VectorThreshold = *update.VectorThreshold
		result.vectorThresholdSet = true
	}
	if update.KeywordThreshold != nil {
		result.KeywordThreshold = *update.KeywordThreshold
		result.keywordThresholdSet = true
	}
	if update.RerankCandidateTopK != nil {
		result.RerankCandidateTopK = *update.RerankCandidateTopK
	}
	if update.RerankTopK != nil {
		result.RerankTopK = *update.RerankTopK
	}
	if update.RerankThreshold != nil {
		result.RerankThreshold = *update.RerankThreshold
		result.rerankThresholdSet = true
	}
	if update.EnableQueryExpansion != nil {
		result.EnableQueryExpansion = *update.EnableQueryExpansion
		result.queryExpansionSet = true
	}
	if update.BatchMaxResults != nil {
		result.BatchMaxResults = *update.BatchMaxResults
	}
	if update.BatchMaxContentChars != nil {
		result.BatchMaxContentChars = *update.BatchMaxContentChars
	}
	result.RerankModelID = ""
	return result
}

// DefaultRetrievalConfig returns the single platform retrieval default.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		EmbeddingTopK:        DefaultEmbeddingTopK,
		VectorRecallTopK:     DefaultVectorRecallTopK,
		KeywordRecallTopK:    DefaultKeywordRecallTopK,
		RRFVectorWeight:      DefaultRRFVectorWeight,
		VectorThreshold:      DefaultVectorThreshold,
		KeywordThreshold:     DefaultKeywordThreshold,
		RerankCandidateTopK:  DefaultRerankCandidateTopK,
		RerankTopK:           DefaultRerankTopK,
		RerankThreshold:      DefaultRerankThreshold,
		EnableQueryExpansion: true,
		BatchMaxResults:      DefaultBatchMaxResults,
		BatchMaxContentChars: DefaultBatchMaxContentChars,
	}
}

// NormalizeRetrievalConfig fills fields absent from legacy persisted JSON.
func NormalizeRetrievalConfig(config *RetrievalConfig) RetrievalConfig {
	result := DefaultRetrievalConfig()
	if config == nil {
		return result
	}
	result = *config
	if result.EmbeddingTopK <= 0 {
		result.EmbeddingTopK = DefaultEmbeddingTopK
	}
	if result.VectorRecallTopK <= 0 {
		result.VectorRecallTopK = DefaultVectorRecallTopK
	}
	if result.KeywordRecallTopK <= 0 {
		result.KeywordRecallTopK = DefaultKeywordRecallTopK
	}
	if result.RRFVectorWeight == 0 && !result.rrfVectorWeightSet {
		result.RRFVectorWeight = DefaultRRFVectorWeight
	}
	if result.RerankCandidateTopK <= 0 {
		result.RerankCandidateTopK = min(DefaultRerankCandidateTopK, result.EmbeddingTopK)
	}
	if result.RerankTopK <= 0 {
		result.RerankTopK = min(DefaultRerankTopK, result.RerankCandidateTopK)
	}
	// Legacy rows store zero for absent thresholds.
	if result.VectorThreshold == 0 && !result.vectorThresholdSet {
		result.VectorThreshold = DefaultVectorThreshold
	}
	if result.KeywordThreshold == 0 && !result.keywordThresholdSet {
		result.KeywordThreshold = DefaultKeywordThreshold
	}
	if result.RerankThreshold == 0 && !result.rerankThresholdSet {
		result.RerankThreshold = DefaultRerankThreshold
	}
	if !result.queryExpansionSet {
		result.EnableQueryExpansion = true
	}
	if result.BatchMaxResults <= 0 {
		result.BatchMaxResults = DefaultBatchMaxResults
	}
	if result.BatchMaxContentChars <= 0 {
		result.BatchMaxContentChars = DefaultBatchMaxContentChars
	}
	result.RerankModelID = ""
	return result
}

// ValidateRetrievalConfig validates a normalized platform retrieval strategy.
func ValidateRetrievalConfig(config RetrievalConfig) error {
	if config.EmbeddingTopK < 1 || config.EmbeddingTopK > 500 {
		return fmt.Errorf("embedding_top_k must be between 1 and 500")
	}
	if config.VectorRecallTopK < 1 || config.VectorRecallTopK > 500 {
		return fmt.Errorf("vector_recall_top_k must be between 1 and 500")
	}
	if config.KeywordRecallTopK < 1 || config.KeywordRecallTopK > 500 {
		return fmt.Errorf("keyword_recall_top_k must be between 1 and 500")
	}
	if config.RRFVectorWeight < 0 || config.RRFVectorWeight > 1 {
		return fmt.Errorf("rrf_vector_weight must be between 0 and 1")
	}
	if config.VectorThreshold < 0 || config.VectorThreshold > 1 {
		return fmt.Errorf("vector_threshold must be between 0 and 1")
	}
	if config.KeywordThreshold < 0 || config.KeywordThreshold > 1 {
		return fmt.Errorf("keyword_threshold must be between 0 and 1")
	}
	if config.RerankCandidateTopK < 1 || config.RerankCandidateTopK > config.EmbeddingTopK {
		return fmt.Errorf("rerank_candidate_top_k must be between 1 and embedding_top_k")
	}
	if config.RerankTopK < 1 || config.RerankTopK > config.RerankCandidateTopK {
		return fmt.Errorf("rerank_top_k must be between 1 and rerank_candidate_top_k")
	}
	if config.RerankThreshold < -10 || config.RerankThreshold > 10 {
		return fmt.Errorf("rerank_threshold must be between -10 and 10")
	}
	if config.BatchMaxResults < 1 || config.BatchMaxResults > MaxBatchMaxResults {
		return fmt.Errorf("batch_max_results must be between 1 and %d", MaxBatchMaxResults)
	}
	if config.BatchMaxContentChars < 1 || config.BatchMaxContentChars > MaxBatchMaxContentChars {
		return fmt.Errorf("batch_max_content_chars must be between 1 and %d", MaxBatchMaxContentChars)
	}
	return nil
}

// GetEffectiveEmbeddingTopK returns EmbeddingTopK with a fallback default.
func (c *RetrievalConfig) GetEffectiveEmbeddingTopK() int {
	return NormalizeRetrievalConfig(c).EmbeddingTopK
}

func (c *RetrievalConfig) GetEffectiveVectorRecallTopK() int {
	return NormalizeRetrievalConfig(c).VectorRecallTopK
}

func (c *RetrievalConfig) GetEffectiveKeywordRecallTopK() int {
	return NormalizeRetrievalConfig(c).KeywordRecallTopK
}

func (c *RetrievalConfig) GetEffectiveRRFVectorWeight() float64 {
	return NormalizeRetrievalConfig(c).RRFVectorWeight
}

// GetEffectiveVectorThreshold returns VectorThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveVectorThreshold() float64 {
	return NormalizeRetrievalConfig(c).VectorThreshold
}

// GetEffectiveKeywordThreshold returns KeywordThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveKeywordThreshold() float64 {
	return NormalizeRetrievalConfig(c).KeywordThreshold
}

// GetEffectiveRerankTopK returns RerankTopK with a fallback default.
func (c *RetrievalConfig) GetEffectiveRerankTopK() int {
	return NormalizeRetrievalConfig(c).RerankTopK
}

func (c *RetrievalConfig) GetEffectiveRerankCandidateTopK() int {
	return NormalizeRetrievalConfig(c).RerankCandidateTopK
}

// GetEffectiveRerankThreshold returns RerankThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveRerankThreshold() float64 {
	return NormalizeRetrievalConfig(c).RerankThreshold
}

func (c *RetrievalConfig) GetEffectiveBatchMaxResults() int {
	return NormalizeRetrievalConfig(c).BatchMaxResults
}

func (c *RetrievalConfig) GetEffectiveBatchMaxContentChars() int {
	return NormalizeRetrievalConfig(c).BatchMaxContentChars
}

// Value implements the driver.Valuer interface for database serialization
func (c RetrievalConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for database deserialization
func (c *RetrievalConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	_, c.rrfVectorWeightSet = fields["rrf_vector_weight"]
	_, c.vectorThresholdSet = fields["vector_threshold"]
	_, c.keywordThresholdSet = fields["keyword_threshold"]
	_, c.rerankThresholdSet = fields["rerank_threshold"]
	_, c.queryExpansionSet = fields["enable_query_expansion"]
	return nil
}
