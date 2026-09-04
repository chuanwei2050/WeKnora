package types

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

const IntegrationPublicFolderName = "公共知识"

// IntegrationPublicFolderID returns the stable virtual folder ID used by the
// integration API for a knowledge base's public knowledge container.
func IntegrationPublicFolderID(knowledgeBaseID string) string {
	sum := sha256.Sum256([]byte(knowledgeBaseID))
	return "public-knowledge-" + hex.EncodeToString(sum[:])
}

// KnowledgeTag represents a tag (category) under a specific knowledge base.
// Tags are scoped by knowledge base (and tenant) and are used to categorize
// Knowledge (documents) and FAQ Chunks.
type KnowledgeTag struct {
	// Unique identifier of the tag (UUID)
	ID string `json:"id"                gorm:"type:varchar(36);primaryKey"`
	// SeqID is an auto-increment integer ID for external API usage
	SeqID int64 `json:"seq_id"            gorm:"type:bigint;uniqueIndex;autoIncrement"`
	// Tenant ID
	TenantID uint64 `json:"tenant_id"`
	// Knowledge base ID that this tag belongs to
	KnowledgeBaseID string `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	// Tag display name; identity is provided by ID, so names may repeat
	Name string `json:"name"              gorm:"type:varchar(128);not null"`
	// Optional display color
	Color string `json:"color"             gorm:"type:varchar(32)"`
	// Sort order within the same knowledge base
	SortOrder int `json:"sort_order"        gorm:"default:0"`
	// Whether this folder is displayed under the virtual public-files container.
	IsPublic bool `json:"is_public" gorm:"not null;default:false"`
	// ParentID is set only for ordinary second-level folders.
	ParentID *string `json:"parent_id,omitempty" gorm:"type:varchar(36);index"`
	// Whether files in this folder can appear in search and conversation retrieval.
	SearchEnabled bool `json:"search_enabled" gorm:"not null;default:true"`
	// Creation time
	CreatedAt time.Time `json:"created_at"`
	// Last updated time
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate ensures SeqID is populated for databases that don't support
// autoIncrement on non-primary-key columns (e.g. SQLite).
// On PostgreSQL/MySQL the DB sequence handles this, so we skip to avoid
// duplicate key races under concurrent inserts.
func (t *KnowledgeTag) BeforeCreate(tx *gorm.DB) error {
	if tx.Dialector.Name() != "sqlite" {
		return nil
	}
	if t.SeqID == 0 {
		var maxSeqID *int64
		tx.Unscoped().Model(&KnowledgeTag{}).
			Select("MAX(seq_id)").
			Scan(&maxSeqID)
		if maxSeqID != nil {
			t.SeqID = *maxSeqID + 1
		} else {
			t.SeqID = 1
		}
	}
	return nil
}

// KnowledgeTagWithStats represents tag information along with usage statistics.
type KnowledgeTagWithStats struct {
	KnowledgeTag
	KnowledgeCount int64 `json:"knowledge_count"`
	ChunkCount     int64 `json:"chunk_count"`
}

// TagReferenceCounts holds the reference counts for a tag.
type TagReferenceCounts struct {
	KnowledgeCount int64
	ChunkCount     int64
}
