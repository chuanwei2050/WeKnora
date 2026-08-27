package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// knowledgeTagRepository is a repository for knowledge tags
type knowledgeTagRepository struct {
	db *gorm.DB
}

// NewKnowledgeTagRepository creates a new tag repository.
func NewKnowledgeTagRepository(db *gorm.DB) interfaces.KnowledgeTagRepository {
	return &knowledgeTagRepository{db: db}
}

// Create creates a new knowledge tag
func (r *knowledgeTagRepository) Create(ctx context.Context, tag *types.KnowledgeTag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

// Update updates a knowledge tag
func (r *knowledgeTagRepository) Update(ctx context.Context, tag *types.KnowledgeTag) error {
	return r.db.WithContext(ctx).Save(tag).Error
}

// GetByID gets a knowledge tag by ID
func (r *knowledgeTagRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.KnowledgeTag, error) {
	var tag types.KnowledgeTag
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// GetByIDs retrieves multiple tags by their IDs in a single query
func (r *knowledgeTagRepository) GetByIDs(ctx context.Context, tenantID uint64, ids []string) ([]*types.KnowledgeTag, error) {
	if len(ids) == 0 {
		return []*types.KnowledgeTag{}, nil
	}
	var tags []*types.KnowledgeTag
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id IN (?)", tenantID, ids).
		Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

// GetBySeqID retrieves a tag by its seq_id
func (r *knowledgeTagRepository) GetBySeqID(ctx context.Context, tenantID uint64, seqID int64) (*types.KnowledgeTag, error) {
	var tag types.KnowledgeTag
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND seq_id = ?", tenantID, seqID).
		First(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// GetBySeqIDs retrieves multiple tags by their seq_ids in a single query
func (r *knowledgeTagRepository) GetBySeqIDs(ctx context.Context, tenantID uint64, seqIDs []int64) ([]*types.KnowledgeTag, error) {
	if len(seqIDs) == 0 {
		return []*types.KnowledgeTag{}, nil
	}
	var tags []*types.KnowledgeTag
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND seq_id IN (?)", tenantID, seqIDs).
		Find(&tags).Error; err != nil {
		return nil, err
	}
	return tags, nil
}

// GetByName gets a knowledge tag by name
func (r *knowledgeTagRepository) GetByName(ctx context.Context, tenantID uint64, kbID string, name string) (*types.KnowledgeTag, error) {
	var tag types.KnowledgeTag
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND name = ?", tenantID, kbID, name).
		First(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListByKB lists knowledge tags by knowledge base ID with pagination and optional keyword filtering.
func (r *knowledgeTagRepository) ListByKB(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	page *types.Pagination,
	keyword string,
) ([]*types.KnowledgeTag, int64, error) {
	if page == nil {
		page = &types.Pagination{}
	}
	keyword = strings.TrimSpace(keyword)

	var total int64
	baseQuery := r.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if keyword != "" {
		escaped := escapeLikeKeyword(keyword)
		baseQuery = baseQuery.Where("name LIKE ?", "%"+escaped+"%")
	}

	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	dataQuery := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if keyword != "" {
		escaped := escapeLikeKeyword(keyword)
		dataQuery = dataQuery.Where("name LIKE ?", "%"+escaped+"%")
	}

	var tags []*types.KnowledgeTag
	if err := dataQuery.
		Order("sort_order ASC, created_at DESC").
		Offset(page.Offset()).
		Limit(page.Limit()).
		Find(&tags).Error; err != nil {
		return nil, 0, err
	}

	return tags, total, nil
}

// Reorder atomically updates ordinary tag sort orders and keeps the default tag first.
func (r *knowledgeTagRepository) Reorder(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	rootIDs, publicIDs []string,
	childOrders map[string][]string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.KnowledgeTag{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND name = ?", tenantID, kbID, types.UntaggedTagName).
			Update("sort_order", -1).Error; err != nil {
			return err
		}
		updateRootSection := func(ids []string, isPublic bool) error {
			for index, id := range ids {
				result := tx.Model(&types.KnowledgeTag{}).
					Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND name <> ? AND parent_id IS NULL AND is_public = ?", tenantID, kbID, id, types.UntaggedTagName, isPublic).
					Update("sort_order", index)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("tag %s is not reorderable", id)
				}
			}
			return nil
		}
		if err := updateRootSection(rootIDs, false); err != nil {
			return err
		}
		if err := updateRootSection(publicIDs, true); err != nil {
			return err
		}
		for parentID, ids := range childOrders {
			for index, id := range ids {
				result := tx.Model(&types.KnowledgeTag{}).
					Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND parent_id = ? AND is_public = ?", tenantID, kbID, id, parentID, false).
					Update("sort_order", index)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("tag %s is not reorderable under parent %s", id, parentID)
				}
			}
		}
		return nil
	})
}

// HasChildren reports whether an ordinary folder has direct children in the same knowledge base.
func (r *knowledgeTagRepository) HasChildren(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	parentID string,
) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND parent_id = ?", tenantID, kbID, parentID).
		Limit(1).
		Count(&count).Error
	return count > 0, err
}

// DeleteEmptySubtree atomically deletes a folder subtree when it contains no knowledge or chunks.
func (r *knowledgeTagRepository) DeleteEmptySubtree(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	rootID string,
) (bool, error) {
	deleted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tags []*types.KnowledgeTag
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
			Find(&tags).Error; err != nil {
			return err
		}

		childrenByParent := make(map[string][]string)
		for _, tag := range tags {
			if tag.ParentID != nil {
				childrenByParent[*tag.ParentID] = append(childrenByParent[*tag.ParentID], tag.ID)
			}
		}
		ids := make([]string, 0)
		pending := []string{rootID}
		visited := make(map[string]struct{})
		for len(pending) > 0 {
			id := pending[0]
			pending = pending[1:]
			if _, exists := visited[id]; exists {
				continue
			}
			visited[id] = struct{}{}
			ids = append(ids, id)
			pending = append(pending, childrenByParent[id]...)
		}

		var knowledgeCount int64
		if err := tx.Model(&types.Knowledge{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id IN ? AND parse_status <> ?", tenantID, kbID, ids, types.ParseStatusDeleting).
			Count(&knowledgeCount).Error; err != nil {
			return err
		}
		var chunkCount int64
		if err := tx.Model(&types.Chunk{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id IN ?", tenantID, kbID, ids).
			Count(&chunkCount).Error; err != nil {
			return err
		}
		if knowledgeCount > 0 || chunkCount > 0 {
			return nil
		}

		for i := len(ids) - 1; i >= 0; i-- {
			if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, ids[i]).
				Delete(&types.KnowledgeTag{}).Error; err != nil {
				return err
			}
		}
		deleted = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelSerializable})
	return deleted, err
}

// Delete deletes a knowledge tag
func (r *knowledgeTagRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.KnowledgeTag{}).Error
}

// CountReferences returns the number of knowledges and chunks that reference this tag
func (r *knowledgeTagRepository) CountReferences(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	tagID string,
) (knowledgeCount int64, chunkCount int64, err error) {
	if err = r.db.WithContext(ctx).
		Model(&types.Knowledge{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND parse_status <> ?",
			tenantID, kbID, tagID, types.ParseStatusDeleting).
		Count(&knowledgeCount).Error; err != nil {
		return
	}
	if err = r.db.WithContext(ctx).
		Model(&types.Chunk{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ?", tenantID, kbID, tagID).
		Count(&chunkCount).Error; err != nil {
		return
	}
	return
}

// tagCountResult is used to scan the result of batch count queries
type tagCountResult struct {
	TagID string `gorm:"column:tag_id"`
	Count int64  `gorm:"column:count"`
}

// BatchCountReferences returns subtree knowledge and chunk counts for multiple tags.
func (r *knowledgeTagRepository) BatchCountReferences(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	tagIDs []string,
) (map[string]types.TagReferenceCounts, error) {
	result := make(map[string]types.TagReferenceCounts)
	if len(tagIDs) == 0 {
		return result, nil
	}

	// Initialize result with zero counts for all tagIDs
	for _, tagID := range tagIDs {
		result[tagID] = types.TagReferenceCounts{}
	}

	var knowledgeCounts []tagCountResult
	if err := r.db.WithContext(ctx).Raw(`
		WITH RECURSIVE tag_tree(root_id, id) AS (
			SELECT id, id FROM knowledge_tags
			WHERE tenant_id = ? AND knowledge_base_id = ? AND id IN ?
			UNION
			SELECT tree.root_id, child.id FROM knowledge_tags child
			JOIN tag_tree tree ON child.parent_id = tree.id
			WHERE child.tenant_id = ? AND child.knowledge_base_id = ?
		)
		SELECT tree.root_id AS tag_id, COUNT(knowledge.id) AS count
		FROM tag_tree tree
		JOIN knowledges knowledge ON knowledge.tag_id = tree.id
		WHERE knowledge.tenant_id = ? AND knowledge.knowledge_base_id = ?
			AND knowledge.parse_status <> ? AND knowledge.deleted_at IS NULL
		GROUP BY tree.root_id`,
		tenantID, kbID, tagIDs, tenantID, kbID,
		tenantID, kbID, types.ParseStatusDeleting,
	).Scan(&knowledgeCounts).Error; err != nil {
		return nil, err
	}
	for _, kc := range knowledgeCounts {
		counts := result[kc.TagID]
		counts.KnowledgeCount = kc.Count
		result[kc.TagID] = counts
	}

	var chunkCounts []tagCountResult
	if err := r.db.WithContext(ctx).Raw(`
		WITH RECURSIVE tag_tree(root_id, id) AS (
			SELECT id, id FROM knowledge_tags
			WHERE tenant_id = ? AND knowledge_base_id = ? AND id IN ?
			UNION
			SELECT tree.root_id, child.id FROM knowledge_tags child
			JOIN tag_tree tree ON child.parent_id = tree.id
			WHERE child.tenant_id = ? AND child.knowledge_base_id = ?
		)
		SELECT tree.root_id AS tag_id, COUNT(chunk.id) AS count
		FROM tag_tree tree
		JOIN chunks chunk ON chunk.tag_id = tree.id
		WHERE chunk.tenant_id = ? AND chunk.knowledge_base_id = ? AND chunk.deleted_at IS NULL
		GROUP BY tree.root_id`,
		tenantID, kbID, tagIDs, tenantID, kbID, tenantID, kbID,
	).Scan(&chunkCounts).Error; err != nil {
		return nil, err
	}
	for _, cc := range chunkCounts {
		counts := result[cc.TagID]
		counts.ChunkCount = cc.Count
		result[cc.TagID] = counts
	}

	return result, nil
}

// DeleteUnusedTags deletes tags that are not referenced by any knowledge or chunk.
// Returns the number of deleted tags.
func (r *knowledgeTagRepository) DeleteUnusedTags(ctx context.Context, tenantID uint64, kbID string) (int64, error) {
	// Delete tags that have no references in both knowledges and chunks tables (excluding soft-deleted records)
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Where("id NOT IN (SELECT DISTINCT tag_id FROM knowledges WHERE tenant_id = ? AND knowledge_base_id = ? AND tag_id IS NOT NULL AND tag_id != '' AND deleted_at IS NULL)", tenantID, kbID).
		Where("id NOT IN (SELECT DISTINCT tag_id FROM chunks WHERE tenant_id = ? AND knowledge_base_id = ? AND tag_id IS NOT NULL AND tag_id != '' AND deleted_at IS NULL)", tenantID, kbID).
		Delete(&types.KnowledgeTag{})
	return result.RowsAffected, result.Error
}
