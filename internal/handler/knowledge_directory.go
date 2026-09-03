package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

const (
	defaultMaxDirectoryDownloadDocuments       = 1000
	defaultMaxDirectoryDownloadBytes     int64 = 2 << 30
)

func directoryDownloadLimits() (int, int64) {
	maxDocuments := defaultMaxDirectoryDownloadDocuments
	maxBytes := defaultMaxDirectoryDownloadBytes
	if value, err := strconv.Atoi(os.Getenv("DIRECTORY_ZIP_MAX_DOCUMENTS")); err == nil && value > 0 {
		maxDocuments = value
	}
	if value, err := strconv.ParseInt(os.Getenv("DIRECTORY_ZIP_MAX_BYTES"), 10, 64); err == nil && value > 0 {
		maxBytes = value
	}
	return maxDocuments, maxBytes
}

type directoryNameRequest struct {
	Name     string  `json:"name" binding:"required"`
	ParentID *string `json:"parent_id"`
	TagID    string  `json:"tag_id"`
}

type moveDirectoryRequest struct {
	ParentID *string `json:"parent_id"`
}

type moveDocumentsToDirectoryRequest struct {
	IDs         []string `json:"ids" binding:"required,max=200"`
	DirectoryID *string  `json:"directory_id"`
}

type moveDirectoryEntriesRequest struct {
	DirectoryIDs []string `json:"directory_ids" binding:"max=200"`
	KnowledgeIDs []string `json:"knowledge_ids" binding:"max=200"`
	DirectoryID  *string  `json:"directory_id"`
}

type moveDirectoriesToTagRequest struct {
	DirectoryIDs []string `json:"directory_ids" binding:"required,max=200"`
	TargetTagID  string   `json:"target_tag_id" binding:"required"`
}

type confirmDirectoryDeleteRequest struct {
	ConfirmationToken string `json:"confirmation_token" binding:"required"`
}

func normalizeOptionalID(id *string) *string {
	if id == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*id)
	if trimmed == "" || trimmed == "__root__" {
		return nil
	}
	return &trimmed
}

func requestedDirectoryTagID(c *gin.Context, bodyTagID string) (string, error) {
	tagID := strings.TrimSpace(bodyTagID)
	if tagID == "" {
		tagID = strings.TrimSpace(c.Query("tag_id"))
	}
	if tagID == "" {
		return "", errors.NewBadRequestError("tag_id is required")
	}
	return tagID, nil
}

func (h *KnowledgeHandler) resolveDirectoryTagID(ctx context.Context, c *gin.Context, kbID, bodyTagID string) (string, error) {
	tagID, err := requestedDirectoryTagID(c, bodyTagID)
	if err != nil {
		return "", err
	}
	resolvedTagID, err := h.kgService.ResolveDocumentTagID(ctx, kbID, tagID)
	if err != nil || resolvedTagID != tagID {
		return "", errors.NewBadRequestError("tag_id does not belong to the knowledge base")
	}
	return resolvedTagID, nil
}

func (h *KnowledgeHandler) directoryContext(c *gin.Context, write bool) (context.Context, string, uint64, error) {
	_, kbID, tenantID, permission, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		return nil, "", 0, err
	}
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	if write && !permission.HasPermission(types.OrgRoleEditor) {
		return nil, "", 0, errors.NewForbiddenError("No permission to organize knowledge")
	}
	if userID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String())); userID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	return ctx, kbID, tenantID, nil
}

func directoryError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "not found") {
		return errors.NewNotFoundError("Document directory not found")
	}
	if strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
		return errors.NewConflictError("A directory with this name already exists")
	}
	if strings.Contains(message, "invalid") || strings.Contains(message, "empty") || strings.Contains(message, "descendant") || strings.Contains(message, "parent") || strings.Contains(message, "limit_exceeded") {
		return errors.NewBadRequestError("Invalid document directory request")
	}
	if strings.Contains(message, "deleting") {
		return errors.NewConflictError("Document directory is being deleted")
	}
	if strings.Contains(message, "unavailable") {
		return errors.NewInternalServerError("Document directory download failed")
	}
	return errors.NewInternalServerError("Document directory operation failed")
}

// validateDocumentMoveAccess checks every document ID before a directory move.
// Directory-level permission alone is not enough because a caller could otherwise
// submit IDs from another knowledge base or tenant in the same batch.
func (h *KnowledgeHandler) validateDocumentMoveAccess(ctx context.Context, tenantID uint64, kbID string, ids []string) error {
	for _, id := range ids {
		knowledge, err := h.kgService.GetKnowledgeByIDOnly(ctx, id)
		if err != nil || knowledge == nil || knowledge.TenantID != tenantID || knowledge.KnowledgeBaseID != kbID || knowledge.ParseStatus == types.ParseStatusDeleting {
			return errors.NewForbiddenError("No permission to move these documents")
		}
	}
	return nil
}

func (h *KnowledgeHandler) ListKnowledgeDirectories(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, false)
	if err != nil {
		c.Error(err)
		return
	}
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	var parentID *string
	if raw, ok := c.GetQuery("parent_id"); ok {
		parentID = normalizeOptionalID(&raw)
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	kb, kbErr := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if kbErr != nil {
		c.Error(errors.NewInternalServerError("Document directory operation failed"))
		return
	}
	directories, total, err := h.directoryService.List(ctx, tenantID, kbID, parentID, &page, c.Query("sort_by"), c.Query("sort_order"), knowledgeVisibilityFilter(ctx, kb), tagID)
	if err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": directories, "total": total, "page": page.GetPage(), "page_size": page.GetPageSize()})
}

func (h *KnowledgeHandler) CreateKnowledgeDirectory(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req directoryNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, req.TagID)
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	directory, err := h.directoryService.Create(ctx, tenantID, kbID, tagID, normalizeOptionalID(req.ParentID), req.Name)
	if err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": directory})
}

func (h *KnowledgeHandler) RenameKnowledgeDirectory(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req directoryNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, req.TagID)
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	if err := h.directoryService.Rename(ctx, tenantID, kbID, tagID, c.Param("directory_id"), req.Name); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *KnowledgeHandler) MoveKnowledgeDirectory(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req moveDirectoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	if err := h.directoryService.Move(ctx, tenantID, kbID, tagID, c.Param("directory_id"), normalizeOptionalID(req.ParentID)); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *KnowledgeHandler) DeleteKnowledgeDirectory(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	if err := h.directoryService.DeleteEmpty(ctx, tenantID, kbID, tagID, c.Param("directory_id")); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *KnowledgeHandler) directoryDeleteContext(c *gin.Context) (context.Context, string, uint64, string, error) {
	_, kbID, tenantID, permission, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		return nil, "", 0, "", err
	}
	if permission != types.OrgRoleAdmin {
		return nil, "", 0, "", errors.NewForbiddenError("No permission to delete document directories")
	}
	userID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if userID == "" {
		return nil, "", 0, "", errors.NewForbiddenError("Missing user identity")
	}
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	return ctx, kbID, tenantID, userID, nil
}

func (h *KnowledgeHandler) PreviewKnowledgeDirectoryDelete(c *gin.Context) {
	ctx, kbID, tenantID, userID, err := h.directoryDeleteContext(c)
	if err != nil {
		c.Error(err)
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	preview, err := h.directoryService.PreviewDelete(ctx, tenantID, kbID, tagID, c.Param("directory_id"), userID)
	if err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": preview})
}

func (h *KnowledgeHandler) ConfirmKnowledgeDirectoryDelete(c *gin.Context) {
	ctx, kbID, tenantID, userID, err := h.directoryDeleteContext(c)
	if err != nil {
		c.Error(err)
		return
	}
	var req confirmDirectoryDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	task, err := h.directoryService.ConfirmDelete(ctx, tenantID, kbID, tagID, c.Param("directory_id"), userID, req.ConfirmationToken)
	if err != nil {
		if task != nil {
			c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "Directory deletion accepted; cleanup is still being dispatched", "data": task})
			return
		}
		if strings.Contains(err.Error(), "directory_changed") || strings.Contains(err.Error(), "confirmation token") {
			c.Error(errors.NewConflictError("Directory deletion confirmation expired or changed"))
			return
		}
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": task})
}

func (h *KnowledgeHandler) GetKnowledgeDirectoryDeleteTask(c *gin.Context) {
	ctx, kbID, tenantID, _, err := h.directoryDeleteContext(c)
	if err != nil {
		c.Error(err)
		return
	}
	task, batches, err := h.directoryService.GetDeleteTask(ctx, tenantID, kbID, c.Param("task_id"))
	if err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"task": task, "batches": batches}})
}

func (h *KnowledgeHandler) RetryKnowledgeDirectoryDeleteTask(c *gin.Context) {
	ctx, kbID, tenantID, _, err := h.directoryDeleteContext(c)
	if err != nil {
		c.Error(err)
		return
	}
	if err := h.directoryService.RetryDeleteTask(ctx, tenantID, kbID, c.Param("task_id")); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true})
}

func (h *KnowledgeHandler) GetKnowledgeDirectoryBreadcrumb(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, false)
	if err != nil {
		c.Error(err)
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	path, err := h.directoryService.Breadcrumb(ctx, tenantID, kbID, tagID, c.Param("directory_id"))
	if err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": path})
}

func (h *KnowledgeHandler) MoveDocumentsToDirectory(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req moveDocumentsToDirectoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	ids := make([]string, 0, len(req.IDs))
	seen := make(map[string]struct{}, len(req.IDs))
	for _, raw := range req.IDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 200 {
		c.Error(errors.NewBadRequestError("ids must contain 1 to 200 documents"))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	if err := h.validateDocumentMoveAccess(ctx, tenantID, kbID, ids); err != nil {
		c.Error(err)
		return
	}
	if err := h.directoryService.MoveEntries(ctx, tenantID, kbID, tagID, nil, ids, normalizeOptionalID(req.DirectoryID)); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"moved_count": len(ids)}})
}

func (h *KnowledgeHandler) MoveKnowledgeDirectoryEntries(c *gin.Context) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req moveDirectoryEntriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	directories := uniqueNonEmptyIDs(req.DirectoryIDs)
	documents := uniqueNonEmptyIDs(req.KnowledgeIDs)
	if len(directories)+len(documents) == 0 || len(directories)+len(documents) > 200 {
		c.Error(errors.NewBadRequestError("move requires 1 to 200 entries"))
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	if err := h.validateDocumentMoveAccess(ctx, tenantID, kbID, documents); err != nil {
		c.Error(err)
		return
	}
	if err := h.directoryService.MoveEntries(ctx, tenantID, kbID, tagID, directories, documents, normalizeOptionalID(req.DirectoryID)); err != nil {
		c.Error(directoryError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"moved_count": len(directories) + len(documents)}})
}

func (h *KnowledgeHandler) MoveKnowledgeDirectoriesToTag(c *gin.Context) {
	ctx, kbID, _, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	var req moveDirectoriesToTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	directories := uniqueNonEmptyIDs(req.DirectoryIDs)
	if len(directories) == 0 || len(directories) > 200 {
		c.Error(errors.NewBadRequestError("directory_ids must contain 1 to 200 directories"))
		return
	}
	sourceTagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}
	targetTagID := strings.TrimSpace(req.TargetTagID)
	resolvedTargetTagID, tagErr := h.kgService.ResolveDocumentTagID(ctx, kbID, targetTagID)
	if tagErr != nil || resolvedTargetTagID != targetTagID {
		c.Error(errors.NewBadRequestError("target_tag_id does not belong to the knowledge base"))
		return
	}
	if sourceTagID == targetTagID {
		c.Error(errors.NewBadRequestError("target category must differ from the current category"))
		return
	}
	movedCount, moveErr := h.kgService.MoveSubtreesToTag(ctx, kbID, sourceTagID, targetTagID, directories)
	if moveErr != nil {
		c.Error(directoryError(moveErr))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"moved_count": movedCount}})
}

func uniqueNonEmptyIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func safeZipEntryName(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	cleaned := path.Clean("/" + name)
	if strings.ContainsRune(name, rune(0)) || strings.HasPrefix(name, "/") || strings.Contains(name, "../") || cleaned == "/" {
		return "", types.ErrInvalidDirectoryPath
	}
	return strings.TrimPrefix(cleaned, "/"), nil
}

func planDirectoryZipEntries(directories []*types.KnowledgeDirectory, knowledges []*types.Knowledge, paths map[string]string) (map[string]string, map[string]string, error) {
	directoryEntries := make(map[string]string, len(directories))
	for _, directory := range directories {
		entryName, err := safeZipEntryName(paths[directory.ID])
		if err != nil {
			return nil, nil, fmt.Errorf("invalid archive path: %w", err)
		}
		directoryEntries[directory.ID] = entryName + "/"
	}
	documentEntries := make(map[string]string, len(knowledges))
	used := make(map[string]int)
	for _, knowledge := range knowledges {
		if knowledge.DirectoryID == nil {
			return nil, nil, fmt.Errorf("invalid document association")
		}
		fileName, err := safeZipEntryName(knowledge.FileName)
		if err != nil || path.Base(fileName) != fileName {
			return nil, nil, fmt.Errorf("invalid file name")
		}
		entryName, err := safeZipEntryName(path.Join(paths[*knowledge.DirectoryID], fileName))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid archive path: %w", err)
		}
		used[entryName]++
		if used[entryName] > 1 {
			ext := path.Ext(entryName)
			entryName = strings.TrimSuffix(entryName, ext) + fmt.Sprintf(" (%d)", used[entryName]) + ext
		}
		documentEntries[knowledge.ID] = entryName
	}
	return directoryEntries, documentEntries, nil
}

func preflightDirectoryDownload(ctx context.Context, knowledges []*types.Knowledge, maxDocuments int, maxBytes int64, open func(context.Context, string) (io.ReadCloser, string, error)) error {
	var totalBytes int64
	for _, knowledge := range knowledges {
		totalBytes += knowledge.FileSize
	}
	if len(knowledges) > maxDocuments || totalBytes > maxBytes {
		return fmt.Errorf("directory_download_limit_exceeded")
	}
	for _, knowledge := range knowledges {
		reader, _, err := open(ctx, knowledge.ID)
		if err != nil {
			return fmt.Errorf("directory_file_unavailable: %w", err)
		}
		if err := reader.Close(); err != nil {
			return fmt.Errorf("directory_file_unavailable: %w", err)
		}
	}
	return nil
}

func (h *KnowledgeHandler) DownloadKnowledgeDirectory(c *gin.Context) {
	h.downloadKnowledgeDirectories(c, []string{c.Param("directory_id")})
}

func (h *KnowledgeHandler) DownloadKnowledgeDirectories(c *gin.Context) {
	rootIDs := uniqueNonEmptyIDs(c.QueryArray("directory_id"))
	if len(rootIDs) == 0 || len(rootIDs) > 200 {
		c.Error(errors.NewBadRequestError("directory_id must contain 1 to 200 directories"))
		return
	}
	h.downloadKnowledgeDirectories(c, rootIDs)
}

func (h *KnowledgeHandler) downloadKnowledgeDirectories(c *gin.Context, requestedRootIDs []string) {
	ctx, kbID, tenantID, err := h.directoryContext(c, true)
	if err != nil {
		c.Error(err)
		return
	}
	tagID, tagErr := h.resolveDirectoryTagID(ctx, c, kbID, "")
	if tagErr != nil {
		c.Error(tagErr)
		return
	}

	rootSet := make(map[string]struct{}, len(requestedRootIDs))
	type downloadRoot struct {
		id   string
		name string
	}
	roots := make([]downloadRoot, 0, len(requestedRootIDs))
	for _, rootID := range requestedRootIDs {
		if _, duplicate := rootSet[rootID]; duplicate {
			continue
		}
		breadcrumb, pathErr := h.directoryService.Breadcrumb(ctx, tenantID, kbID, tagID, rootID)
		if pathErr != nil {
			c.Error(directoryError(pathErr))
			return
		}
		rootSet[rootID] = struct{}{}
		if len(breadcrumb) == 0 {
			c.Error(errors.NewNotFoundError("Document directory not found"))
			return
		}
		roots = append(roots, downloadRoot{id: rootID, name: breadcrumb[len(breadcrumb)-1].Name})
	}
	if len(roots) == 0 {
		c.Error(errors.NewBadRequestError("directory_id must contain 1 to 200 directories"))
		return
	}

	// Selecting a parent and one of its children downloads the parent once.
	keptRoots := make([]downloadRoot, 0, len(roots))
	for _, root := range roots {
		breadcrumb, pathErr := h.directoryService.Breadcrumb(ctx, tenantID, kbID, tagID, root.id)
		if pathErr != nil {
			c.Error(directoryError(pathErr))
			return
		}
		nested := false
		for _, node := range breadcrumb[:len(breadcrumb)-1] {
			if _, selected := rootSet[node.ID]; selected {
				nested = true
				break
			}
		}
		if !nested {
			keptRoots = append(keptRoots, root)
		}
	}

	archiveRootNames := make(map[string]string, len(keptRoots))
	usedRootNames := make(map[string]int, len(keptRoots))
	usedArchiveNames := make(map[string]struct{}, len(keptRoots))
	for _, root := range keptRoots {
		archiveName := ""
		for suffix := usedRootNames[root.name] + 1; ; suffix++ {
			candidate := root.name
			if suffix > 1 {
				candidate = fmt.Sprintf("%s (%d)", root.name, suffix)
			}
			if _, alreadyUsed := usedArchiveNames[candidate]; alreadyUsed {
				continue
			}
			archiveName = candidate
			usedRootNames[root.name] = suffix
			break
		}
		usedArchiveNames[archiveName] = struct{}{}
		archiveRootNames[root.id] = archiveName
	}

	directoryByID := make(map[string]*types.KnowledgeDirectory)
	knowledgeByID := make(map[string]*types.Knowledge)
	paths := make(map[string]string)
	for _, root := range keptRoots {
		directories, knowledges, subtreeErr := h.directoryService.ListSubtree(ctx, tenantID, kbID, tagID, root.id)
		if subtreeErr != nil {
			c.Error(directoryError(subtreeErr))
			return
		}
		for _, directory := range directories {
			directoryByID[directory.ID] = directory
			breadcrumb, pathErr := h.directoryService.Breadcrumb(ctx, tenantID, kbID, tagID, directory.ID)
			if pathErr != nil {
				c.Error(directoryError(pathErr))
				return
			}
			start := 0
			for start < len(breadcrumb) && breadcrumb[start].ID != root.id {
				start++
			}
			if start == len(breadcrumb) {
				c.Error(errors.NewBadRequestError("Document directory path is invalid"))
				return
			}
			parts := []string{archiveRootNames[root.id]}
			for _, node := range breadcrumb[start+1:] {
				parts = append(parts, node.Name)
			}
			paths[directory.ID] = strings.Join(parts, "/")
		}
		for _, knowledge := range knowledges {
			knowledgeByID[knowledge.ID] = knowledge
		}
	}

	kb, kbErr := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if kbErr != nil {
		c.Error(errors.NewInternalServerError(kbErr.Error()))
		return
	}
	visible := make([]*types.Knowledge, 0, len(knowledgeByID))
	for _, knowledge := range knowledgeByID {
		if h.canViewGovernedKnowledge(ctx, knowledge, kb) {
			visible = append(visible, knowledge)
		}
	}
	maxDocuments, maxBytes := directoryDownloadLimits()
	if preflightErr := preflightDirectoryDownload(ctx, visible, maxDocuments, maxBytes, h.kgService.GetKnowledgeFile); preflightErr != nil {
		if strings.Contains(preflightErr.Error(), "limit_exceeded") {
			c.Error(errors.NewBadRequestError("Document directory is too large to download"))
		} else {
			c.Error(errors.NewInternalServerError("One or more files are unavailable"))
		}
		return
	}

	directories := make([]*types.KnowledgeDirectory, 0, len(directoryByID))
	for _, directory := range directoryByID {
		directories = append(directories, directory)
	}
	knowledges := make([]*types.Knowledge, 0, len(visible))
	for _, knowledge := range visible {
		knowledges = append(knowledges, knowledge)
	}
	sort.Slice(directories, func(i, j int) bool { return paths[directories[i].ID] < paths[directories[j].ID] })
	sort.Slice(knowledges, func(i, j int) bool { return knowledges[i].ID < knowledges[j].ID })
	directoryEntries, documentEntries, planErr := planDirectoryZipEntries(directories, knowledges, paths)
	if planErr != nil {
		c.Error(errors.NewBadRequestError("Document directory contains an invalid archive path"))
		return
	}
	archiveName := "文档目录"
	if len(keptRoots) == 1 {
		archiveName = keptRoots[0].name
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": archiveName + ".zip"}))
	writer := zip.NewWriter(c.Writer)
	defer writer.Close()
	for _, directory := range directories {
		_, _ = writer.Create(directoryEntries[directory.ID])
	}
	for _, knowledge := range knowledges {
		fresh, loadErr := h.kgService.GetKnowledgeByID(ctx, knowledge.ID)
		if loadErr != nil || fresh.KnowledgeBaseID != kbID || fresh.TagID != tagID || fresh.DirectoryID == nil {
			return
		}
		if _, belongsToSubtree := paths[*fresh.DirectoryID]; !belongsToSubtree || !h.canViewGovernedKnowledge(ctx, fresh, kb) {
			return
		}
		entry, createErr := writer.Create(documentEntries[knowledge.ID])
		if createErr != nil {
			return
		}
		reader, _, openErr := h.kgService.GetKnowledgeFile(ctx, fresh.ID)
		if openErr != nil {
			return
		}
		_, copyErr := io.Copy(entry, reader)
		_ = reader.Close()
		if copyErr != nil {
			return
		}
	}
}
