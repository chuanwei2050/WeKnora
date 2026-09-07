package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

type integrationKnowledgeChunkReader interface {
	ReadIntegrationKnowledgeChunks(context.Context, uint64, string, string, int, int) ([]*types.Chunk, int64, error)
}

// ReadKnowledgeChunks is a bounded, ordered read of an explicitly authorized
// document revision. It does not treat top-k retrieval as a complete document.
func (h *IntegrationHandler) ReadKnowledgeChunks(c *gin.Context) {
	if !h.enforceRate(c, "api.knowledge.read", 120) || !h.requireScope(c, "knowledge:read") {
		return
	}
	h.limitRequestBody(c)
	var req struct {
		KnowledgeBaseID string    `json:"knowledge_base_id" binding:"required"`
		KnowledgeID     string    `json:"knowledge_id" binding:"required"`
		UpdatedAt       time.Time `json:"updated_at" binding:"required"`
		FolderIDs       []string  `json:"folder_ids"`
		Page            int       `json:"page"`
		PageSize        int       `json:"page_size"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Page < 1 || req.Page > 1000000 || req.PageSize < 1 || req.PageSize > 50 ||
		!validIntegrationKnowledgeIDs(req.FolderIDs, h.limits.maxKnowledgeIDs) {
		integrationError(c, http.StatusBadRequest, "invalid_document_read", "revision, page and page_size (1-50) are required")
		return
	}
	principal := integrationPrincipal(c)
	if h.service.AuthorizeKnowledgeBases(principal, []string{req.KnowledgeBaseID}) != nil {
		integrationError(c, http.StatusForbidden, "knowledge_base_denied", "knowledge base access denied")
		return
	}
	knowledge, err := h.knowledges.GetKnowledgeByIDOnly(c.Request.Context(), req.KnowledgeID)
	if err != nil || knowledge == nil || knowledge.TenantID != principal.TenantID || knowledge.KnowledgeBaseID != req.KnowledgeBaseID {
		integrationError(c, http.StatusNotFound, "knowledge_not_found", "knowledge not found in requested scope")
		return
	}
	if !knowledge.UpdatedAt.Equal(req.UpdatedAt) {
		integrationError(c, http.StatusConflict, "knowledge_revision_changed", "knowledge revision changed; refresh the document inventory")
		return
	}
	if knowledge.EnableStatus != "enabled" {
		integrationError(c, http.StatusForbidden, "knowledge_disabled", "knowledge is disabled")
		return
	}
	folders, ok := h.knowledges.(integrationSearchableFolderProvider)
	if !ok {
		integrationError(c, http.StatusServiceUnavailable, "folder_scope_unavailable", "folder scope unavailable")
		return
	}
	searchable, err := folders.SearchableTagIDs(c.Request.Context(), principal.TenantID, req.KnowledgeBaseID)
	allowed := false
	if err == nil {
		for _, id := range searchable {
			if id == knowledge.TagID {
				allowed = true
			}
		}
	}
	if allowed && len(req.FolderIDs) > 0 {
		resolver, ok := h.knowledges.(integrationFolderProvider)
		if !ok {
			allowed = false
		} else {
			resolved, resolveErr := resolver.ResolveIntegrationFolderIDs(c.Request.Context(), principal.TenantID, []string{req.KnowledgeBaseID}, req.FolderIDs, nil)
			allowed = false
			if resolveErr == nil {
				for _, id := range resolved {
					if id == knowledge.TagID {
						allowed = true
					}
				}
			}
		}
	}
	if !allowed {
		integrationError(c, http.StatusForbidden, "folder_scope_denied", "knowledge folder is disabled or outside requested scope")
		return
	}
	reader, ok := h.knowledges.(integrationKnowledgeChunkReader)
	if !ok {
		integrationError(c, http.StatusServiceUnavailable, "document_read_unavailable", "document read unavailable")
		return
	}
	rows, total, err := reader.ReadIntegrationKnowledgeChunks(c.Request.Context(), principal.TenantID, req.KnowledgeID, knowledge.CurrentVersionID, req.Page, req.PageSize)
	if err != nil {
		integrationError(c, http.StatusInternalServerError, "document_read_failed", "document read failed")
		return
	}
	// Reject a concurrent reparse instead of joining different revisions.
	latest, err := h.knowledges.GetKnowledgeByIDOnly(c.Request.Context(), req.KnowledgeID)
	if err != nil || latest == nil || !latest.UpdatedAt.Equal(req.UpdatedAt) {
		integrationError(c, http.StatusConflict, "knowledge_revision_changed", "knowledge revision changed during read")
		return
	}
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.TenantID != principal.TenantID || row.KnowledgeID != req.KnowledgeID || row.KnowledgeBaseID != req.KnowledgeBaseID {
			integrationError(c, http.StatusInternalServerError, "invalid_document_scope", "invalid document read scope")
			return
		}
		if !row.IsEnabled {
			continue
		}
		if knowledge.CurrentVersionID != "" && row.KnowledgeVersionID != knowledge.CurrentVersionID {
			continue
		}
		sum := sha256.Sum256([]byte(row.Content))
		result = append(result, gin.H{"id": row.ID, "content": row.Content, "chunk_index": row.ChunkIndex,
			"start_at": row.StartAt, "end_at": row.EndAt, "sha256": hex.EncodeToString(sum[:])})
	}
	var next any
	if int64(req.Page)*int64(req.PageSize) < total {
		next = req.Page + 1
	}
	h.service.AuditResources(c.Request.Context(), principal, "api.knowledge.read", "allowed", "", []string{req.KnowledgeBaseID})
	integrationData(c, http.StatusOK, gin.H{"schema_version": "knowledge-document-page/v1",
		"knowledge_base_id": req.KnowledgeBaseID, "knowledge_id": req.KnowledgeID, "updated_at": req.UpdatedAt,
		"page": req.Page, "page_size": req.PageSize, "total_chunks": total, "next_page": next, "chunks": result})
}
