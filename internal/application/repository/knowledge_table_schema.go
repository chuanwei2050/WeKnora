package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type knowledgeTableSchemaRepository struct{ db *gorm.DB }

const maxTableSchemaBytes = 2 << 20

func NewKnowledgeTableSchemaRepository(db *gorm.DB) interfaces.KnowledgeTableSchemaRepository {
	return &knowledgeTableSchemaRepository{db: db}
}

func deleteKnowledgeTableSchemas(db *gorm.DB, query string, args ...any) error {
	if !db.Migrator().HasTable(&types.KnowledgeTableSchema{}) {
		return nil
	}
	return db.Where(query, args...).Delete(&types.KnowledgeTableSchema{}).Error
}

func tableSchemaRevision(knowledge *types.Knowledge) string {
	if knowledge == nil {
		return ""
	}
	if versionID := strings.TrimSpace(knowledge.PendingVersionID); versionID != "" {
		return "version:" + versionID
	}
	if versionID := strings.TrimSpace(knowledge.CurrentVersionID); versionID != "" {
		return "version:" + versionID
	}
	return "file:" + strings.TrimSpace(knowledge.FileHash)
}

func currentTableSchemaRevision(knowledge *types.Knowledge) string {
	if knowledge == nil {
		return ""
	}
	if versionID := strings.TrimSpace(knowledge.CurrentVersionID); versionID != "" {
		return "version:" + versionID
	}
	return "file:" + strings.TrimSpace(knowledge.FileHash)
}

func (r *knowledgeTableSchemaRepository) Upsert(ctx context.Context, knowledge *types.Knowledge, schemaJSON []byte) error {
	revision := tableSchemaRevision(knowledge)
	if knowledge == nil || revision == "file:" || len(schemaJSON) == 0 {
		return nil
	}
	if len(schemaJSON) > maxTableSchemaBytes || !json.Valid(schemaJSON) {
		return fmt.Errorf("invalid table schema payload")
	}
	schemaJSON, err := removeTableSchemaValueExamples(schemaJSON)
	if err != nil {
		return err
	}
	record := &types.KnowledgeTableSchema{
		TenantID: knowledge.TenantID, KnowledgeID: knowledge.ID, Revision: revision,
		KnowledgeVersionID: strings.TrimSpace(knowledge.PendingVersionID), FileHash: strings.TrimSpace(knowledge.FileHash),
		SchemaJSON: types.JSON(append([]byte(nil), schemaJSON...)),
	}
	if record.KnowledgeVersionID == "" {
		record.KnowledgeVersionID = strings.TrimSpace(knowledge.CurrentVersionID)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND id = ?", knowledge.TenantID, knowledge.ID).First(&current).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}
		if tableSchemaRevision(&current) != revision {
			return nil
		}
		switch current.ParseStatus {
		case types.ParseStatusFailed, types.ParseStatusRejected, types.ParseStatusDeleting:
			return nil
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "knowledge_id"}, {Name: "revision"}},
			DoUpdates: clause.AssignmentColumns([]string{"knowledge_version_id", "file_hash", "schema_json", "updated_at"}),
		}).Create(record).Error
	})
}

func removeTableSchemaValueExamples(schemaJSON []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(schemaJSON, &payload); err != nil {
		return nil, fmt.Errorf("decode table schema payload: %w", err)
	}
	if columns, ok := payload["columns"].([]any); ok {
		for _, item := range columns {
			if column, ok := item.(map[string]any); ok {
				delete(column, "value_examples")
			}
		}
	}
	return json.Marshal(payload)
}

func (r *knowledgeTableSchemaRepository) GetCurrent(ctx context.Context, knowledge *types.Knowledge) ([]byte, bool, error) {
	revision := currentTableSchemaRevision(knowledge)
	if knowledge == nil || revision == "file:" {
		return nil, false, nil
	}
	switch knowledge.ParseStatus {
	case types.ParseStatusFailed, types.ParseStatusRejected, types.ParseStatusDeleting:
		return nil, false, nil
	}
	var record types.KnowledgeTableSchema
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_id = ? AND revision = ?", knowledge.TenantID, knowledge.ID, revision).First(&record).Error
	if err == gorm.ErrRecordNotFound {
		return nil, false, nil
	}
	return append([]byte(nil), record.SchemaJSON...), err == nil, err
}

func (r *knowledgeTableSchemaRepository) DeleteKnowledge(ctx context.Context, tenantID uint64, knowledgeID string) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_id = ?", tenantID, knowledgeID).Delete(&types.KnowledgeTableSchema{}).Error
}

func (r *knowledgeTableSchemaRepository) ListMissing(ctx context.Context, tenantID uint64, limit int) ([]*types.Knowledge, error) {
	var rows []*types.Knowledge
	revisionSQL := "CASE WHEN COALESCE(k.current_version_id, '') <> '' THEN 'version:' || k.current_version_id ELSE 'file:' || COALESCE(k.file_hash, '') END"
	query := r.db.WithContext(ctx).Table("knowledges k").Select("k.*").
		Where("k.deleted_at IS NULL AND LOWER(k.file_type) IN ?", []string{"csv", "xls", "xlsx"}).
		Where("NOT EXISTS (SELECT 1 FROM knowledge_table_schemas s WHERE s.tenant_id = k.tenant_id AND s.knowledge_id = k.id AND s.revision = " + revisionSQL + ")")
	if tenantID != 0 {
		query = query.Where("k.tenant_id = ?", tenantID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Order("k.file_size ASC").Find(&rows).Error
	return rows, err
}
