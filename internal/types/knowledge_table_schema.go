package types

import "time"

// KnowledgeTableSchema stores the authoritative schema extracted from a table file.
type KnowledgeTableSchema struct {
	TenantID           uint64 `gorm:"primaryKey"`
	KnowledgeID        string `gorm:"primaryKey;type:varchar(36)"`
	Revision           string `gorm:"primaryKey;type:varchar(128)"`
	KnowledgeVersionID string `gorm:"type:varchar(36);index"`
	FileHash           string `gorm:"type:varchar(128)"`
	SchemaJSON         JSON   `gorm:"column:schema_json;type:json"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (KnowledgeTableSchema) TableName() string { return "knowledge_table_schemas" }
