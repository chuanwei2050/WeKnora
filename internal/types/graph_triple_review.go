package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// GraphTripleReviewStatus is independent from KnowledgeVersionStatus.
type GraphTripleReviewStatus string

const (
	GraphTriplePending    GraphTripleReviewStatus = "pending"
	GraphTripleWritten    GraphTripleReviewStatus = "written"
	GraphTripleRejected   GraphTripleReviewStatus = "rejected"
	GraphTripleSuperseded GraphTripleReviewStatus = "superseded"
)

// GraphTripleCandidate is a staging record for human triple review before Neo4j write.
type GraphTripleCandidate struct {
	ID                 string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID           uint64                  `json:"tenant_id" gorm:"index:idx_graph_triple_scope;not null"`
	KnowledgeBaseID    string                  `json:"knowledge_base_id" gorm:"type:varchar(36);index:idx_graph_triple_scope;not null"`
	KnowledgeID        string                  `json:"knowledge_id" gorm:"type:varchar(36);not null"`
	KnowledgeVersionID string                  `json:"knowledge_version_id,omitempty" gorm:"type:varchar(36)"`
	ChunkID            string                  `json:"chunk_id" gorm:"type:varchar(36);index:idx_graph_triple_chunk;not null"`
	ModelID            string                  `json:"model_id,omitempty" gorm:"type:varchar(36)"`
	GraphData          GraphDataPayload        `json:"graph_data" gorm:"type:jsonb;not null"`
	Status             GraphTripleReviewStatus `json:"status" gorm:"type:varchar(16);index:idx_graph_triple_scope;not null"`
	ReviewerID         string                  `json:"reviewer_id,omitempty" gorm:"type:varchar(36)"`
	Comment            string                  `json:"comment,omitempty" gorm:"type:text"`
	SupersededBy       string                  `json:"superseded_by,omitempty" gorm:"type:varchar(36)"`
	CreatedAt          time.Time               `json:"created_at"`
	ReviewedAt         *time.Time              `json:"reviewed_at,omitempty"`
	WrittenAt          *time.Time              `json:"written_at,omitempty"`
}

func (GraphTripleCandidate) TableName() string { return "graph_triple_candidates" }

// GraphDataPayload persists filtered GraphData as JSON.
type GraphDataPayload GraphData

func (p GraphDataPayload) Value() (driver.Value, error) {
	return json.Marshal(GraphData(p))
}

func (p *GraphDataPayload) Scan(value interface{}) error {
	if value == nil {
		*p = GraphDataPayload{}
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("unsupported graph payload type %T", value)
	}
	var data GraphData
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	*p = GraphDataPayload(data)
	return nil
}

func (p GraphDataPayload) AsGraphData() *GraphData {
	data := GraphData(p)
	return &data
}

func ValidateGraphTripleTransition(from, to GraphTripleReviewStatus) error {
	switch from {
	case GraphTriplePending:
		switch to {
		case GraphTripleWritten, GraphTripleRejected, GraphTripleSuperseded:
			return nil
		}
	}
	return fmt.Errorf("invalid graph triple transition %s -> %s", from, to)
}

func CanApproveGraphTriple(status GraphTripleReviewStatus) bool {
	return status == GraphTriplePending
}

func CanRejectGraphTriple(status GraphTripleReviewStatus) bool {
	return status == GraphTriplePending
}
