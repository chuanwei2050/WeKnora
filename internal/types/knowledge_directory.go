package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	DirectoryStatusActive   = "active"
	DirectoryStatusDeleting = "deleting"
)

// KnowledgeDirectory is an organizational node in the right-hand document browser.
// It is scoped to a KnowledgeTag for browsing, but never participates in retrieval scope.
type KnowledgeDirectory struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id" gorm:"uniqueIndex:uq_directory_sibling,priority:1;index:idx_directory_parent,priority:1"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36);uniqueIndex:uq_directory_sibling,priority:2;index:idx_directory_parent,priority:2"`
	TagID           string     `json:"tag_id" gorm:"type:varchar(36);not null;uniqueIndex:uq_directory_sibling,priority:3;index:idx_directory_parent,priority:3"`
	ParentID        *string    `json:"parent_id,omitempty" gorm:"type:varchar(36)"`
	ParentKey       string     `json:"-" gorm:"type:varchar(36);not null;default:'';uniqueIndex:uq_directory_sibling,priority:4;index:idx_directory_parent,priority:4"`
	Name            string     `json:"name" gorm:"type:varchar(255);not null"`
	NormalizedName  string     `json:"-" gorm:"type:varchar(255);not null;uniqueIndex:uq_directory_sibling,priority:5"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null;default:active;index:idx_directory_parent,priority:5"`
	DeletionTaskID  string     `json:"deletion_task_id,omitempty" gorm:"type:varchar(36);index"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DocumentCount   int64      `json:"document_count" gorm:"-"`
	Breadcrumb      []PathNode `json:"breadcrumb,omitempty" gorm:"-"`
}

type PathNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type KnowledgeListEntry struct {
	Kind      string              `json:"kind"`
	Directory *KnowledgeDirectory `json:"directory,omitempty"`
	Document  *Knowledge          `json:"document,omitempty"`
}

const (
	DirectoryDeleteStatusPending   = "pending"
	DirectoryDeleteStatusRunning   = "running"
	DirectoryDeleteStatusCompleted = "completed"
	DirectoryDeleteStatusFailed    = "failed"
)

type KnowledgeDirectoryDeleteTask struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36)"`
	RootDirectoryID string    `json:"root_directory_id" gorm:"type:varchar(36)"`
	RequestedBy     string    `json:"requested_by" gorm:"type:varchar(36)"`
	SnapshotDigest  string    `json:"-" gorm:"type:varchar(64)"`
	Status          string    `json:"status" gorm:"type:varchar(32)"`
	FailureSummary  string    `json:"failure_summary,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type KnowledgeDirectoryDeleteBatch struct {
	ID             string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	DeleteTaskID   string      `json:"delete_task_id" gorm:"type:varchar(36);index"`
	AsynqTaskID    string      `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	KnowledgeIDs   StringArray `json:"knowledge_ids" gorm:"type:json"`
	Status         string      `json:"status" gorm:"type:varchar(32)"`
	FailureSummary string      `json:"failure_summary,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type KnowledgeDirectoryDeleteToken struct {
	TokenHash       string     `json:"-" gorm:"type:varchar(64);primaryKey"`
	TenantID        uint64     `json:"-"`
	KnowledgeBaseID string     `json:"-" gorm:"type:varchar(36)"`
	RootDirectoryID string     `json:"-" gorm:"type:varchar(36)"`
	RequestedBy     string     `json:"-" gorm:"type:varchar(36)"`
	SnapshotDigest  string     `json:"-" gorm:"type:varchar(64)"`
	ExpiresAt       time.Time  `json:"-"`
	ConsumedAt      *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"-"`
}

type KnowledgeDirectoryDeletePreview struct {
	DirectoryCount    int       `json:"directory_count"`
	DocumentCount     int       `json:"document_count"`
	TotalStorageSize  int64     `json:"total_storage_size"`
	ConfirmationToken string    `json:"confirmation_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (d *KnowledgeDirectory) BeforeCreate(_ *gorm.DB) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	if d.ParentID != nil {
		d.ParentKey = *d.ParentID
	}
	if d.Status == "" {
		d.Status = DirectoryStatusActive
	}
	return nil
}
