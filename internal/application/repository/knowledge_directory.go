package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrKnowledgeDirectoryNotFound = errors.New("knowledge directory not found")

type knowledgeDirectoryRepository struct{ db *gorm.DB }

func NewKnowledgeDirectoryRepository(db *gorm.DB) interfaces.KnowledgeDirectoryRepository {
	return &knowledgeDirectoryRepository{db: db}
}

func directoryParentKey(parentID *string) string {
	if parentID == nil {
		return ""
	}
	return *parentID
}

func directoryTagID(tagIDs []string) string {
	if len(tagIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(tagIDs[0])
}

func scopeDirectoryTag(query *gorm.DB, tagID string) *gorm.DB {
	if tagID == "" {
		return query
	}
	return query.Where("tag_id = ?", tagID)
}

func directoryTransaction(ctx context.Context, db *gorm.DB, tenantID uint64, kbID string, fn func(*gorm.DB) error) error {
	db = db.WithContext(ctx)
	if db.Dialector.Name() == "sqlite" {
		for attempt := 0; ; attempt++ {
			err := db.Connection(func(conn *gorm.DB) error {
				fresh := func() *gorm.DB {
					return conn.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true}).WithContext(ctx)
				}
				if err := fresh().Exec("BEGIN IMMEDIATE").Error; err != nil {
					return err
				}
				committed := false
				defer func() {
					if !committed {
						_ = fresh().Exec("ROLLBACK").Error
					}
				}()
				var sentinel string
				if err := fresh().Table("knowledge_bases").Select("id").Where("tenant_id = ? AND id = ?", tenantID, kbID).Scan(&sentinel).Error; err != nil {
					return err
				}
				if sentinel == "" {
					return fmt.Errorf("knowledge base not found")
				}
				if err := fn(fresh()); err != nil {
					return err
				}
				if err := fresh().Exec("COMMIT").Error; err != nil {
					return err
				}
				committed = true
				return nil
			})
			if err == nil || attempt >= 99 || (!strings.Contains(strings.ToLower(err.Error()), "locked") && !strings.Contains(strings.ToLower(err.Error()), "busy")) {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var sentinel string
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Table("knowledge_bases").Select("id").Where("tenant_id = ? AND id = ?", tenantID, kbID).Scan(&sentinel).Error; err != nil {
			return err
		}
		if sentinel == "" {
			return fmt.Errorf("knowledge base not found")
		}
		return fn(tx)
	})
}

func (r *knowledgeDirectoryRepository) Create(ctx context.Context, directory *types.KnowledgeDirectory) error {
	return directoryTransaction(ctx, r.db, directory.TenantID, directory.KnowledgeBaseID, func(tx *gorm.DB) error {
		ancestor := directory.ParentID
		for ancestor != nil {
			var parent types.KnowledgeDirectory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ? AND status = ?", directory.TenantID, directory.KnowledgeBaseID, directory.TagID, *ancestor, types.DirectoryStatusActive).First(&parent).Error; err != nil {
				return err
			}
			ancestor = parent.ParentID
		}
		return tx.Create(directory).Error
	})
}

func (r *knowledgeDirectoryRepository) Get(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) (*types.KnowledgeDirectory, error) {
	var directory types.KnowledgeDirectory
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id)
	err := scopeDirectoryTag(query, directoryTagID(tagIDs)).First(&directory).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrKnowledgeDirectoryNotFound
	}
	return &directory, err
}

func (r *knowledgeDirectoryRepository) FindChild(ctx context.Context, tenantID uint64, kbID string, parentID *string, normalizedName string, tagIDs ...string) (*types.KnowledgeDirectory, error) {
	var directory types.KnowledgeDirectory
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND parent_key = ? AND normalized_name = ?", tenantID, kbID, directoryParentKey(parentID), normalizedName)
	err := scopeDirectoryTag(query, directoryTagID(tagIDs)).First(&directory).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrKnowledgeDirectoryNotFound
	}
	return &directory, err
}

func (r *knowledgeDirectoryRepository) ListChildren(ctx context.Context, tenantID uint64, kbID string, parentID *string, offset, limit int, sortBy, sortOrder string, visibility types.KnowledgeVisibilityFilter, tagIDs ...string) ([]*types.KnowledgeDirectory, int64, error) {
	query := r.db.WithContext(ctx).Model(&types.KnowledgeDirectory{}).Where("tenant_id = ? AND knowledge_base_id = ? AND parent_key = ? AND status = ?", tenantID, kbID, directoryParentKey(parentID), types.DirectoryStatusActive)
	tagID := directoryTagID(tagIDs)
	query = scopeDirectoryTag(query, tagID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var directories []*types.KnowledgeDirectory
	direction := "ASC"
	if strings.EqualFold(sortOrder, "desc") {
		direction = "DESC"
	}
	order := "normalized_name " + direction
	if sortBy == "updated_at" {
		order = "updated_at " + direction
	}
	err := query.Order(order).Order("id " + direction).Offset(offset).Limit(limit).Find(&directories).Error
	if err != nil {
		return nil, 0, err
	}
	for _, directory := range directories {
		countQuery := r.db.WithContext(ctx).Model(&types.Knowledge{}).Where(`tenant_id = ? AND knowledge_base_id = ? AND parse_status <> ? AND directory_id IN (
			WITH RECURSIVE directory_tree(id) AS (SELECT id FROM knowledge_directories WHERE id = ? AND tenant_id = ? AND knowledge_base_id = ?
			UNION ALL SELECT child.id FROM knowledge_directories child JOIN directory_tree parent ON child.parent_id = parent.id
			WHERE child.tenant_id = ? AND child.knowledge_base_id = ?) SELECT id FROM directory_tree)`, tenantID, kbID, types.ParseStatusDeleting, directory.ID, tenantID, kbID, tenantID, kbID)
		if tagID != "" {
			countQuery = countQuery.Where("tag_id = ?", tagID)
		}
		countQuery = applyKnowledgeVisibility(countQuery, visibility)
		if err := countQuery.Count(&directory.DocumentCount).Error; err != nil {
			return nil, 0, err
		}
	}
	return directories, total, err
}

func (r *knowledgeDirectoryRepository) Rename(ctx context.Context, tenantID uint64, kbID, id, name, normalizedName string, tagIDs ...string) error {
	tagID := directoryTagID(tagIDs)
	return directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var directory types.KnowledgeDirectory
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, id, types.DirectoryStatusActive)
		if err := scopeDirectoryTag(query, tagID).First(&directory).Error; err != nil {
			return err
		}
		ancestor := directory.ParentID
		for ancestor != nil {
			var parent types.KnowledgeDirectory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, *ancestor, types.DirectoryStatusActive).First(&parent).Error; err != nil {
				return err
			}
			ancestor = parent.ParentID
		}
		return tx.Model(&directory).Updates(map[string]any{"name": name, "normalized_name": normalizedName}).Error
	})
}

func (r *knowledgeDirectoryRepository) Move(ctx context.Context, tenantID uint64, kbID, id string, parentID *string, tagIDs ...string) error {
	tagID := directoryTagID(tagIDs)
	return directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var source types.KnowledgeDirectory
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, id, types.DirectoryStatusActive)
		if err := scopeDirectoryTag(query, tagID).First(&source).Error; err != nil {
			return err
		}
		if parentID != nil {
			if *parentID == id {
				return fmt.Errorf("directory cannot be its own parent")
			}
			current := *parentID
			for current != "" {
				var parent types.KnowledgeDirectory
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ? AND status = ?", tenantID, kbID, source.TagID, current, types.DirectoryStatusActive).First(&parent).Error; err != nil {
					return err
				}
				if parent.ID == id {
					return fmt.Errorf("directory cannot be moved into its descendant")
				}
				if parent.ParentID == nil {
					break
				}
				current = *parent.ParentID
			}
		}
		return tx.Model(&source).Updates(map[string]any{"parent_id": parentID, "parent_key": directoryParentKey(parentID)}).Error
	})
}

func (r *knowledgeDirectoryRepository) MoveEntries(ctx context.Context, tenantID uint64, kbID string, directoryIDs, knowledgeIDs []string, parentID *string, tagIDs ...string) error {
	tagID := directoryTagID(tagIDs)
	return directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var targetAncestors []*types.KnowledgeDirectory
		current := parentID
		for current != nil {
			var directory types.KnowledgeDirectory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ? AND status = ?", tenantID, kbID, tagID, *current, types.DirectoryStatusActive).First(&directory).Error; err != nil {
				return err
			}
			targetAncestors = append(targetAncestors, &directory)
			current = directory.ParentID
			if len(targetAncestors) > types.MaxDirectoryDepth {
				return types.ErrInvalidDirectoryPath
			}
		}
		var sources []*types.KnowledgeDirectory
		if len(directoryIDs) > 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id IN ? AND status = ?", tenantID, kbID, tagID, directoryIDs, types.DirectoryStatusActive).Order("id ASC").Find(&sources).Error; err != nil {
				return err
			}
			if len(sources) != len(directoryIDs) {
				return fmt.Errorf("one or more directories are invalid")
			}
		}
		sourceByID := make(map[string]*types.KnowledgeDirectory, len(sources))
		for _, source := range sources {
			sourceByID[source.ID] = source
		}
		for _, ancestor := range targetAncestors {
			if _, selected := sourceByID[ancestor.ID]; selected {
				return fmt.Errorf("directory cannot be moved into its descendant")
			}
		}
		collapsed := make([]*types.KnowledgeDirectory, 0, len(sources))
		for _, source := range sources {
			ancestorID := source.ParentID
			nested := false
			for ancestorID != nil {
				if _, selected := sourceByID[*ancestorID]; selected {
					nested = true
					break
				}
				var ancestor types.KnowledgeDirectory
				if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ?", tenantID, kbID, tagID, *ancestorID).First(&ancestor).Error; err != nil {
					return err
				}
				ancestorID = ancestor.ParentID
			}
			if !nested {
				collapsed = append(collapsed, source)
			}
		}
		names := make(map[string]struct{}, len(collapsed))
		for _, source := range collapsed {
			if source.ParentKey == directoryParentKey(parentID) {
				continue
			}
			if _, duplicate := names[source.NormalizedName]; duplicate {
				return fmt.Errorf("duplicate directory name at destination")
			}
			names[source.NormalizedName] = struct{}{}
			var count int64
			query := tx.Model(&types.KnowledgeDirectory{}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND parent_key = ? AND normalized_name = ? AND status = ?", tenantID, kbID, tagID, directoryParentKey(parentID), source.NormalizedName, types.DirectoryStatusActive)
			if err := query.Where("id NOT IN ?", directoryIDs).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("duplicate directory name at destination")
			}
		}
		if len(knowledgeIDs) > 0 {
			var lockedKnowledgeIDs []string
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Model(&types.Knowledge{}).
				Select("id").
				Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id IN ? AND parse_status <> ?", tenantID, kbID, tagID, knowledgeIDs, types.ParseStatusDeleting).
				Order("id ASC").
				Pluck("id", &lockedKnowledgeIDs).Error; err != nil {
				return err
			}
			if len(lockedKnowledgeIDs) != len(knowledgeIDs) {
				return fmt.Errorf("one or more documents are invalid")
			}
		}
		for _, source := range collapsed {
			if source.ParentKey == directoryParentKey(parentID) {
				continue
			}
			if err := tx.Model(&types.KnowledgeDirectory{}).Where("id = ? AND status = ?", source.ID, types.DirectoryStatusActive).Updates(map[string]any{"parent_id": parentID, "parent_key": directoryParentKey(parentID)}).Error; err != nil {
				return err
			}
		}
		if len(knowledgeIDs) > 0 {
			if err := tx.Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id IN ?", tenantID, kbID, tagID, knowledgeIDs).Update("directory_id", parentID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *knowledgeDirectoryRepository) MoveSubtreesToTag(ctx context.Context, tenantID uint64, kbID, sourceTagID, targetTagID string, directoryIDs, directKnowledgeIDs []string) ([]string, error) {
	movedKnowledgeIDs := make([]string, 0)
	if sourceTagID == targetTagID {
		return movedKnowledgeIDs, fmt.Errorf("source and target categories must differ")
	}
	err := directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var sources []*types.KnowledgeDirectory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id IN ? AND status = ?",
			tenantID, kbID, sourceTagID, directoryIDs, types.DirectoryStatusActive,
		).Order("id ASC").Find(&sources).Error; err != nil {
			return err
		}
		if len(sources) != len(directoryIDs) {
			return fmt.Errorf("one or more directories are invalid")
		}
		sourceByID := make(map[string]*types.KnowledgeDirectory, len(sources))
		for _, source := range sources {
			sourceByID[source.ID] = source
		}
		collapsed := make([]*types.KnowledgeDirectory, 0, len(sources))
		for _, source := range sources {
			ancestorID := source.ParentID
			nested := false
			for ancestorID != nil {
				if _, selected := sourceByID[*ancestorID]; selected {
					nested = true
					break
				}
				var ancestor types.KnowledgeDirectory
				if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ? AND status = ?", tenantID, kbID, sourceTagID, *ancestorID, types.DirectoryStatusActive).First(&ancestor).Error; err != nil {
					return err
				}
				ancestorID = ancestor.ParentID
			}
			if !nested {
				collapsed = append(collapsed, source)
			}
		}
		if len(collapsed) == 0 {
			return fmt.Errorf("one or more directories are invalid")
		}

		allDirectoryIDs := make([]string, 0)
		seenDirectoryIDs := make(map[string]struct{})
		originalParentIDs := make(map[string]string)
		documentDirectoryIDs := make(map[string]string)
		seenKnowledgeIDs := make(map[string]struct{})
		rootNames := make(map[string]struct{}, len(collapsed))
		for _, root := range collapsed {
			if _, duplicate := rootNames[root.NormalizedName]; duplicate {
				return fmt.Errorf("duplicate directory name at destination")
			}
			rootNames[root.NormalizedName] = struct{}{}
			var count int64
			if err := tx.Model(&types.KnowledgeDirectory{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND parent_key = ? AND normalized_name = ?",
				tenantID, kbID, targetTagID, "", root.NormalizedName,
			).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return fmt.Errorf("duplicate directory name at destination")
			}
			directories, knowledges, err := listDirectorySubtree(tx, tenantID, kbID, root.ID, sourceTagID)
			if err != nil {
				return err
			}
			for _, directory := range directories {
				if directory.Status != types.DirectoryStatusActive {
					return fmt.Errorf("one or more directories are deleting")
				}
			}
			for _, directory := range directories {
				if _, seen := seenDirectoryIDs[directory.ID]; !seen {
					seenDirectoryIDs[directory.ID] = struct{}{}
					allDirectoryIDs = append(allDirectoryIDs, directory.ID)
					if directory.ParentID != nil {
						originalParentIDs[directory.ID] = *directory.ParentID
					}
				}
			}
			for _, knowledge := range knowledges {
				if knowledge.ParseStatus == types.ParseStatusDeleting {
					return fmt.Errorf("one or more documents are deleting")
				}
				if _, seen := seenKnowledgeIDs[knowledge.ID]; !seen {
					seenKnowledgeIDs[knowledge.ID] = struct{}{}
					movedKnowledgeIDs = append(movedKnowledgeIDs, knowledge.ID)
				}
				if knowledge.DirectoryID != nil {
					documentDirectoryIDs[knowledge.ID] = *knowledge.DirectoryID
				}
			}
		}
		if len(directKnowledgeIDs) > 0 {
			var directDocuments []*types.Knowledge
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id IN ? AND parse_status <> ?",
				tenantID, kbID, sourceTagID, directKnowledgeIDs, types.ParseStatusDeleting,
			).Order("id ASC").Find(&directDocuments).Error; err != nil {
				return err
			}
			if len(directDocuments) != len(directKnowledgeIDs) {
				return fmt.Errorf("one or more documents are invalid")
			}
			for _, knowledge := range directDocuments {
				if _, seen := seenKnowledgeIDs[knowledge.ID]; seen {
					continue
				}
				seenKnowledgeIDs[knowledge.ID] = struct{}{}
				movedKnowledgeIDs = append(movedKnowledgeIDs, knowledge.ID)
			}
		}
		// Composite foreign keys include tag_id. Temporarily detach the moved
		// graph so databases with immediate FK checks can update both sides in
		// one transaction, then restore the exact same hierarchy.
		if len(movedKnowledgeIDs) > 0 {
			if err := tx.Model(&types.Knowledge{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND tag_id = ?",
				tenantID, kbID, movedKnowledgeIDs, sourceTagID,
			).Update("directory_id", nil).Error; err != nil {
				return err
			}
		}
		if len(originalParentIDs) > 0 {
			if err := tx.Model(&types.KnowledgeDirectory{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND tag_id = ?",
				tenantID, kbID, allDirectoryIDs, sourceTagID,
			).Update("parent_id", nil).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&types.KnowledgeDirectory{}).Where(
			"tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND tag_id = ? AND status = ?",
			tenantID, kbID, allDirectoryIDs, sourceTagID, types.DirectoryStatusActive,
		).Update("tag_id", targetTagID).Error; err != nil {
			return err
		}
		for _, root := range collapsed {
			if err := tx.Model(&types.KnowledgeDirectory{}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, root.ID, types.DirectoryStatusActive).Updates(map[string]any{"parent_id": nil, "parent_key": ""}).Error; err != nil {
				return err
			}
		}
		for directoryID, parentID := range originalParentIDs {
			if err := tx.Model(&types.KnowledgeDirectory{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND id = ? AND tag_id = ?",
				tenantID, kbID, directoryID, targetTagID,
			).Update("parent_id", parentID).Error; err != nil {
				return err
			}
		}
		if len(movedKnowledgeIDs) > 0 {
			if err := tx.Model(&types.Knowledge{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND tag_id = ? AND parse_status <> ?",
				tenantID, kbID, movedKnowledgeIDs, sourceTagID, types.ParseStatusDeleting,
			).Update("tag_id", targetTagID).Error; err != nil {
				return err
			}
			for knowledgeID, directoryID := range documentDirectoryIDs {
				if err := tx.Model(&types.Knowledge{}).Where(
					"tenant_id = ? AND knowledge_base_id = ? AND id = ? AND tag_id = ?",
					tenantID, kbID, knowledgeID, targetTagID,
				).Update("directory_id", directoryID).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&types.Chunk{}).Where(
				"tenant_id = ? AND knowledge_base_id = ? AND knowledge_id IN ?",
				tenantID, kbID, movedKnowledgeIDs,
			).Update("tag_id", targetTagID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(movedKnowledgeIDs)
	return movedKnowledgeIDs, nil
}

func (r *knowledgeDirectoryRepository) DeleteEmpty(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) error {
	tagID := directoryTagID(tagIDs)
	return directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var directory types.KnowledgeDirectory
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, id, types.DirectoryStatusActive)
		if err := scopeDirectoryTag(query, tagID).First(&directory).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrKnowledgeDirectoryNotFound
			}
			return err
		}
		for ancestor := directory.ParentID; ancestor != nil; {
			var parent types.KnowledgeDirectory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, *ancestor, types.DirectoryStatusActive).First(&parent).Error; err != nil {
				return err
			}
			ancestor = parent.ParentID
		}
		var count int64
		if err := tx.Model(&types.KnowledgeDirectory{}).Where("tenant_id = ? AND knowledge_base_id = ? AND parent_id = ?", tenantID, kbID, id).Count(&count).Error; err != nil || count != 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("directory is not empty")
		}
		if err := tx.Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND directory_id = ?", tenantID, kbID, id).Count(&count).Error; err != nil || count != 0 {
			if err != nil {
				return err
			}
			return fmt.Errorf("directory is not empty")
		}
		result := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND id = ? AND status = ?", tenantID, kbID, id, types.DirectoryStatusActive).Delete(&types.KnowledgeDirectory{})
		if result.Error == nil && result.RowsAffected != 1 {
			return ErrKnowledgeDirectoryNotFound
		}
		return result.Error
	})
}

func (r *knowledgeDirectoryRepository) DeleteByTag(ctx context.Context, tenantID uint64, kbID, tagID string) error {
	return directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		if err := tx.Model(&types.Knowledge{}).
			Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ?", tenantID, kbID, tagID).
			Update("directory_id", nil).Error; err != nil {
			return err
		}
		var directories []*types.KnowledgeDirectory
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ?", tenantID, kbID, tagID).
			Find(&directories).Error; err != nil {
			return err
		}
		depth := make(map[string]int, len(directories))
		byID := make(map[string]*types.KnowledgeDirectory, len(directories))
		for _, directory := range directories {
			byID[directory.ID] = directory
		}
		var resolveDepth func(string) int
		resolveDepth = func(id string) int {
			if value, ok := depth[id]; ok {
				return value
			}
			directory := byID[id]
			if directory == nil || directory.ParentID == nil {
				depth[id] = 0
				return 0
			}
			value := resolveDepth(*directory.ParentID) + 1
			depth[id] = value
			return value
		}
		sort.Slice(directories, func(i, j int) bool {
			return resolveDepth(directories[i].ID) > resolveDepth(directories[j].ID)
		})
		for _, directory := range directories {
			if err := tx.Delete(&types.KnowledgeDirectory{}, "id = ? AND tenant_id = ? AND knowledge_base_id = ? AND tag_id = ?", directory.ID, tenantID, kbID, tagID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *knowledgeDirectoryRepository) Breadcrumb(ctx context.Context, tenantID uint64, kbID, id string, tagIDs ...string) ([]types.PathNode, error) {
	tagID := directoryTagID(tagIDs)
	path := make([]types.PathNode, 0)
	current := id
	for current != "" {
		directory, err := r.Get(ctx, tenantID, kbID, current, tagID)
		if err != nil {
			return nil, err
		}
		path = append(path, types.PathNode{ID: directory.ID, Name: directory.Name})
		if directory.ParentID == nil {
			break
		}
		current = *directory.ParentID
		if len(path) > types.MaxDirectoryDepth {
			return nil, types.ErrInvalidDirectoryPath
		}
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path, nil
}

func (r *knowledgeDirectoryRepository) EnsurePath(ctx context.Context, tenantID uint64, kbID string, parentID *string, segments []string, tagIDs ...string) (*types.KnowledgeDirectory, error) {
	tagID := directoryTagID(tagIDs)
	var current *types.KnowledgeDirectory
	err := directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		currentParent := parentID
		ancestor := parentID
		for ancestor != nil {
			var directory types.KnowledgeDirectory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND id = ? AND status = ?", tenantID, kbID, tagID, *ancestor, types.DirectoryStatusActive).First(&directory).Error; err != nil {
				return err
			}
			ancestor = directory.ParentID
		}
		for _, segment := range segments {
			displayName, normalizedName, err := types.NormalizeDirectoryName(segment)
			if err != nil {
				return err
			}
			var directory types.KnowledgeDirectory
			err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND tag_id = ? AND parent_key = ? AND normalized_name = ?", tenantID, kbID, tagID, directoryParentKey(currentParent), normalizedName).First(&directory).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				directory = types.KnowledgeDirectory{TenantID: tenantID, KnowledgeBaseID: kbID, TagID: tagID, ParentID: currentParent, Name: displayName, NormalizedName: normalizedName}
				if err := tx.Create(&directory).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if directory.Status != types.DirectoryStatusActive {
				return fmt.Errorf("document directory is deleting")
			}
			current = &directory
			id := directory.ID
			currentParent = &id
		}
		return nil
	})
	return current, err
}

func (r *knowledgeDirectoryRepository) ListSubtree(ctx context.Context, tenantID uint64, kbID, rootID string, tagIDs ...string) ([]*types.KnowledgeDirectory, []*types.Knowledge, error) {
	directories, knowledges, err := listDirectorySubtree(r.db.WithContext(ctx), tenantID, kbID, rootID, directoryTagID(tagIDs))
	if err != nil {
		return nil, nil, err
	}
	active := knowledges[:0]
	for _, knowledge := range knowledges {
		if knowledge.ParseStatus != types.ParseStatusDeleting {
			active = append(active, knowledge)
		}
	}
	return directories, active, nil
}

func listDirectorySubtree(db *gorm.DB, tenantID uint64, kbID, rootID string, tagIDs ...string) ([]*types.KnowledgeDirectory, []*types.Knowledge, error) {
	tagID := directoryTagID(tagIDs)
	var directories []*types.KnowledgeDirectory
	cte := `id IN (WITH RECURSIVE directory_tree(id) AS (
		SELECT id FROM knowledge_directories WHERE tenant_id = ? AND knowledge_base_id = ? AND id = ?
		UNION ALL SELECT child.id FROM knowledge_directories child JOIN directory_tree parent ON child.parent_id = parent.id
		WHERE child.tenant_id = ? AND child.knowledge_base_id = ?) SELECT id FROM directory_tree)`
	query := db.Where(cte, tenantID, kbID, rootID, tenantID, kbID)
	if tagID != "" {
		query = query.Where("tag_id = ?", tagID)
	}
	if err := query.Order("parent_key ASC, normalized_name ASC, id ASC").Find(&directories).Error; err != nil {
		return nil, nil, err
	}
	if len(directories) == 0 {
		return nil, nil, ErrKnowledgeDirectoryNotFound
	}
	ids := make([]string, 0, len(directories))
	for _, directory := range directories {
		ids = append(ids, directory.ID)
	}
	var knowledges []*types.Knowledge
	knowledgeQuery := db.Where("tenant_id = ? AND knowledge_base_id = ? AND directory_id IN ?", tenantID, kbID, ids)
	if tagID != "" {
		knowledgeQuery = knowledgeQuery.Where("tag_id = ?", tagID)
	}
	if err := knowledgeQuery.Order("directory_id ASC, file_name ASC, id ASC").Find(&knowledges).Error; err != nil {
		return nil, nil, err
	}
	return directories, knowledges, nil
}

func directorySnapshotDigest(directories []*types.KnowledgeDirectory, knowledges []*types.Knowledge) string {
	parts := make([]string, 0, len(directories)+len(knowledges))
	for _, directory := range directories {
		parts = append(parts, "d:"+directory.ID+":"+directory.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	for _, knowledge := range knowledges {
		parts = append(parts, "k:"+knowledge.ID+":"+knowledge.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func (r *knowledgeDirectoryRepository) CreateDeleteToken(ctx context.Context, token *types.KnowledgeDirectoryDeleteToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

func (r *knowledgeDirectoryRepository) ConfirmDelete(ctx context.Context, tenantID uint64, kbID, rootID, requestedBy, tokenHash string, now time.Time) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error) {
	var task *types.KnowledgeDirectoryDeleteTask
	var batches []*types.KnowledgeDirectoryDeleteBatch
	err := directoryTransaction(ctx, r.db, tenantID, kbID, func(tx *gorm.DB) error {
		var token types.KnowledgeDirectoryDeleteToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ? AND tenant_id = ? AND knowledge_base_id = ? AND root_directory_id = ? AND requested_by = ?", tokenHash, tenantID, kbID, rootID, requestedBy).First(&token).Error; err != nil {
			return fmt.Errorf("invalid confirmation token: %w", err)
		}
		if token.ConsumedAt != nil || !token.ExpiresAt.After(now) {
			return fmt.Errorf("invalid or expired confirmation token")
		}
		directories, knowledges, err := listDirectorySubtree(tx, tenantID, kbID, rootID)
		if err != nil {
			return err
		}
		directoryIDs := make([]string, 0, len(directories))
		for _, directory := range directories {
			directoryIDs = append(directoryIDs, directory.ID)
		}
		sort.Strings(directoryIDs)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", tenantID, kbID, directoryIDs).Order("id ASC").Find(&[]types.KnowledgeDirectory{}).Error; err != nil {
			return err
		}
		directories, knowledges, err = listDirectorySubtree(tx, tenantID, kbID, rootID)
		if err != nil {
			return err
		}
		if directorySnapshotDigest(directories, knowledges) != token.SnapshotDigest {
			return fmt.Errorf("directory_changed")
		}
		task = &types.KnowledgeDirectoryDeleteTask{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, RootDirectoryID: rootID, RequestedBy: requestedBy, SnapshotDigest: token.SnapshotDigest, Status: types.DirectoryDeleteStatusPending}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		consume := tx.Model(&types.KnowledgeDirectoryDeleteToken{}).Where("token_hash = ? AND consumed_at IS NULL", tokenHash).Update("consumed_at", now)
		if consume.Error != nil {
			return consume.Error
		}
		if consume.RowsAffected != 1 {
			return fmt.Errorf("invalid or consumed confirmation token")
		}
		if err := tx.Model(&types.KnowledgeDirectory{}).Where("id IN ? AND status = ?", directoryIDs, types.DirectoryStatusActive).Updates(map[string]any{"status": types.DirectoryStatusDeleting, "deletion_task_id": task.ID}).Error; err != nil {
			return err
		}
		knowledgeIDs := make([]string, 0, len(knowledges))
		for _, knowledge := range knowledges {
			knowledgeIDs = append(knowledgeIDs, knowledge.ID)
		}
		if len(knowledgeIDs) > 0 {
			if err := tx.Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ?", tenantID, kbID, knowledgeIDs).Updates(map[string]any{"parse_status": types.ParseStatusDeleting, "deletion_task_id": task.ID}).Error; err != nil {
				return err
			}
		}
		for start := 0; start < len(knowledgeIDs); start += 200 {
			end := start + 200
			if end > len(knowledgeIDs) {
				end = len(knowledgeIDs)
			}
			batchID := uuid.NewString()
			batch := &types.KnowledgeDirectoryDeleteBatch{ID: batchID, DeleteTaskID: task.ID, AsynqTaskID: "directory-delete-" + batchID, KnowledgeIDs: types.StringArray(knowledgeIDs[start:end]), Status: types.DirectoryDeleteStatusPending}
			if err := tx.Create(batch).Error; err != nil {
				return err
			}
			batches = append(batches, batch)
		}
		if len(batches) == 0 {
			if err := tx.Where("id IN ?", directoryIDs).Delete(&types.KnowledgeDirectory{}).Error; err != nil {
				return err
			}
			task.Status = types.DirectoryDeleteStatusCompleted
			return tx.Model(task).Update("status", task.Status).Error
		}
		return nil
	})
	return task, batches, err
}

func (r *knowledgeDirectoryRepository) GetDeleteTask(ctx context.Context, tenantID uint64, kbID, taskID string) (*types.KnowledgeDirectoryDeleteTask, []*types.KnowledgeDirectoryDeleteBatch, error) {
	var task types.KnowledgeDirectoryDeleteTask
	if err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", taskID, tenantID, kbID).First(&task).Error; err != nil {
		return nil, nil, err
	}
	var batches []*types.KnowledgeDirectoryDeleteBatch
	if err := r.db.WithContext(ctx).Where("delete_task_id = ?", taskID).Order("created_at ASC, id ASC").Find(&batches).Error; err != nil {
		return nil, nil, err
	}
	return &task, batches, nil
}

func (r *knowledgeDirectoryRepository) ValidateDeleteBatch(ctx context.Context, payload *types.KnowledgeListDeletePayload) error {
	var batch types.KnowledgeDirectoryDeleteBatch
	if err := r.db.WithContext(ctx).Where("id = ? AND delete_task_id = ?", payload.DirectoryDeleteBatchID, payload.DirectoryDeleteTaskID).First(&batch).Error; err != nil {
		return err
	}
	var task types.KnowledgeDirectoryDeleteTask
	if err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", payload.DirectoryDeleteTaskID, payload.TenantID, payload.KnowledgeBaseID).First(&task).Error; err != nil {
		return err
	}
	expected, actual := append([]string(nil), batch.KnowledgeIDs...), append([]string(nil), payload.KnowledgeIDs...)
	sort.Strings(expected)
	sort.Strings(actual)
	if string(mustJSON(expected)) != string(mustJSON(actual)) {
		return fmt.Errorf("directory delete batch scope mismatch")
	}
	var count int64
	if err := r.db.WithContext(ctx).Unscoped().Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND deletion_task_id = ?", payload.TenantID, payload.KnowledgeBaseID, actual, payload.DirectoryDeleteTaskID).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(actual)) {
		return fmt.Errorf("directory delete batch knowledge scope mismatch")
	}
	return nil
}

func (r *knowledgeDirectoryRepository) IsDeleteBatchClean(ctx context.Context, payload *types.KnowledgeListDeletePayload) (bool, error) {
	var active int64
	err := r.db.WithContext(ctx).Model(&types.Knowledge{}).Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND deletion_task_id = ?", payload.TenantID, payload.KnowledgeBaseID, payload.KnowledgeIDs, payload.DirectoryDeleteTaskID).Count(&active).Error
	return active == 0, err
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func (r *knowledgeDirectoryRepository) CompleteDeleteBatch(ctx context.Context, taskID, batchID string, executionErr error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		status, summary := types.DirectoryDeleteStatusCompleted, ""
		if executionErr != nil {
			status, summary = types.DirectoryDeleteStatusFailed, executionErr.Error()
		}
		if err := tx.Model(&types.KnowledgeDirectoryDeleteBatch{}).Where("id = ? AND delete_task_id = ?", batchID, taskID).Updates(map[string]any{"status": status, "failure_summary": summary}).Error; err != nil {
			return err
		}
		if executionErr != nil {
			return tx.Model(&types.KnowledgeDirectoryDeleteTask{}).Where("id = ?", taskID).Updates(map[string]any{"status": types.DirectoryDeleteStatusFailed, "failure_summary": summary}).Error
		}
		var remaining int64
		if err := tx.Model(&types.KnowledgeDirectoryDeleteBatch{}).Where("delete_task_id = ? AND status <> ?", taskID, types.DirectoryDeleteStatusCompleted).Count(&remaining).Error; err != nil || remaining > 0 {
			return err
		}
		if err := tx.Unscoped().Model(&types.Knowledge{}).Where("deletion_task_id = ?", taskID).Updates(map[string]any{"directory_id": nil, "deletion_task_id": ""}).Error; err != nil {
			return err
		}
		var directories []*types.KnowledgeDirectory
		if err := tx.Where("deletion_task_id = ?", taskID).Order("created_at DESC").Find(&directories).Error; err != nil {
			return err
		}
		for len(directories) > 0 {
			removed := false
			for index := len(directories) - 1; index >= 0; index-- {
				result := tx.Where("id = ? AND NOT EXISTS (SELECT 1 FROM knowledge_directories child WHERE child.parent_id = knowledge_directories.id)", directories[index].ID).Delete(&types.KnowledgeDirectory{})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 1 {
					directories = append(directories[:index], directories[index+1:]...)
					removed = true
				}
			}
			if !removed {
				return fmt.Errorf("failed to delete directory tree")
			}
		}
		return tx.Model(&types.KnowledgeDirectoryDeleteTask{}).Where("id = ?", taskID).Updates(map[string]any{"status": types.DirectoryDeleteStatusCompleted, "failure_summary": ""}).Error
	})
}

func (r *knowledgeDirectoryRepository) MarkDeleteBatchDispatched(ctx context.Context, taskID, batchID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.KnowledgeDirectoryDeleteBatch{}).
			Where("id = ? AND delete_task_id = ? AND status = ?", batchID, taskID, types.DirectoryDeleteStatusPending).
			Update("status", types.DirectoryDeleteStatusRunning).Error; err != nil {
			return err
		}
		return tx.Model(&types.KnowledgeDirectoryDeleteTask{}).Where("id = ? AND status = ?", taskID, types.DirectoryDeleteStatusPending).Update("status", types.DirectoryDeleteStatusRunning).Error
	})
}

func (r *knowledgeDirectoryRepository) ListPendingDeleteBatches(ctx context.Context, limit int) ([]*types.KnowledgeDirectoryDeleteTask, map[string][]*types.KnowledgeDirectoryDeleteBatch, error) {
	if limit <= 0 {
		limit = 100
	}
	var batches []*types.KnowledgeDirectoryDeleteBatch
	if err := r.db.WithContext(ctx).Where("status = ?", types.DirectoryDeleteStatusPending).Order("created_at ASC, id ASC").Limit(limit).Find(&batches).Error; err != nil {
		return nil, nil, err
	}
	byTask := make(map[string][]*types.KnowledgeDirectoryDeleteBatch)
	taskIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, batch := range batches {
		byTask[batch.DeleteTaskID] = append(byTask[batch.DeleteTaskID], batch)
		if _, exists := seen[batch.DeleteTaskID]; !exists {
			seen[batch.DeleteTaskID] = struct{}{}
			taskIDs = append(taskIDs, batch.DeleteTaskID)
		}
	}
	if len(taskIDs) == 0 {
		return nil, byTask, nil
	}
	var tasks []*types.KnowledgeDirectoryDeleteTask
	if err := r.db.WithContext(ctx).Where("id IN ?", taskIDs).Order("created_at ASC, id ASC").Find(&tasks).Error; err != nil {
		return nil, nil, err
	}
	return tasks, byTask, nil
}

func (r *knowledgeDirectoryRepository) RetryFailedDeleteBatches(ctx context.Context, tenantID uint64, kbID, taskID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task types.KnowledgeDirectoryDeleteTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", taskID, tenantID, kbID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != types.DirectoryDeleteStatusFailed {
			return fmt.Errorf("delete task is not failed")
		}
		var failed []*types.KnowledgeDirectoryDeleteBatch
		if err := tx.Where("delete_task_id = ? AND status = ?", taskID, types.DirectoryDeleteStatusFailed).Find(&failed).Error; err != nil {
			return err
		}
		if len(failed) == 0 {
			return fmt.Errorf("delete task has no failed batches")
		}
		for _, batch := range failed {
			if err := tx.Model(batch).Updates(map[string]any{"status": types.DirectoryDeleteStatusPending, "failure_summary": "", "asynq_task_id": "directory-delete-" + batch.ID + "-retry-" + uuid.NewString()}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&task).Updates(map[string]any{"status": types.DirectoryDeleteStatusPending, "failure_summary": ""}).Error
	})
}
