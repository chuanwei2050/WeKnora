package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type reviewTestKnowledgeService struct {
	interfaces.KnowledgeService
	knowledge *types.Knowledge
}

func (s reviewTestKnowledgeService) GetKnowledgeByID(_ context.Context, id string) (*types.Knowledge, error) {
	if s.knowledge != nil && s.knowledge.ID == id {
		return s.knowledge, nil
	}
	return nil, nil
}

func (s reviewTestKnowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return nil
}

func withGovernanceReviewContext(c *gin.Context, tenantID uint64, userID string) {
	c.Set(types.TenantIDContextKey.String(), tenantID)
	c.Set(types.UserIDContextKey.String(), userID)
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	c.Request = c.Request.WithContext(ctx)
}

type reviewTestKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
	kbs []*types.KnowledgeBase
}

func (s reviewTestKnowledgeBaseService) ListKnowledgeBases(_ context.Context) ([]*types.KnowledgeBase, error) {
	return s.kbs, nil
}

func (s reviewTestKnowledgeBaseService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	for _, kb := range s.kbs {
		if kb != nil && kb.ID == id {
			return kb, nil
		}
	}
	return nil, nil
}

func openGovernanceRejectTestDB(t *testing.T) (*gorm.DB, *types.KnowledgeVersion) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE knowledges (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			knowledge_base_id TEXT NOT NULL,
			parse_status TEXT NOT NULL DEFAULT 'draft',
			pending_version_id TEXT,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE knowledge_versions (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			knowledge_id TEXT NOT NULL,
			version_label TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			snapshot_ref TEXT,
			source_metadata TEXT NOT NULL DEFAULT '{}',
			previous_version_id TEXT,
			status TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			effective_at DATETIME,
			expires_at DATETIME
		)`,
		`CREATE TABLE knowledge_version_reviews (
			id TEXT PRIMARY KEY,
			version_id TEXT NOT NULL,
			reviewer_id TEXT NOT NULL,
			action TEXT NOT NULL,
			comment TEXT,
			created_at DATETIME NOT NULL
		)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	version := &types.KnowledgeVersion{
		ID: "version-pending", TenantID: 1, KnowledgeID: "doc-1", VersionLabel: "v1",
		ContentHash: types.HashKnowledgeContent([]byte("pending")),
		SourceMetadata: types.KnowledgeSourceMetadata{
			Layer: types.KnowledgeLayerFoundation, SourceCategory: "test", AuthorityLevel: "test",
		},
		Status: types.KnowledgeVersionPendingReview, CreatedBy: "author", CreatedAt: time.Now().UTC(),
	}
	repo := repository.NewKnowledgeGovernanceRepository(db, nil)
	require.NoError(t, repo.CreateVersion(context.Background(), version))
	require.NoError(t, db.Exec(
		"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, parse_status, pending_version_id) VALUES (?, ?, ?, ?, ?)",
		"doc-1", 1, "kb-1", types.ParseStatusPendingReview, version.ID,
	).Error)
	return db, version
}

func TestRejectRequiresNonEmptyComment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, version := openGovernanceRejectTestDB(t)
	repo := repository.NewKnowledgeGovernanceRepository(db, nil)
	kb := &types.KnowledgeBase{
		ID: "kb-1", TenantID: 1,
		Governance:  types.KnowledgeGovernanceConfig{Enabled: true, ProfileID: "p", ProfileVersion: "1"},
		ReviewerIDs: types.StringArray{"reviewer"},
	}
	h := NewKnowledgeGovernanceHandler(repo, reviewTestKnowledgeService{
		knowledge: &types.Knowledge{ID: "doc-1", TenantID: 1, KnowledgeBaseID: "kb-1", PendingVersionID: version.ID, CreatedBy: "author"},
	}, reviewTestKnowledgeBaseService{kbs: []*types.KnowledgeBase{kb}})

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.POST("/knowledge/:id/versions/:version_id/reject", func(c *gin.Context) {
		withGovernanceReviewContext(c, 1, "reviewer")
		h.Reject(c)
	})

	for _, body := range []string{`{}`, `{"comment":""}`, `{"comment":"   "}`} {
		req := httptest.NewRequest(http.MethodPost, "/knowledge/doc-1/versions/"+version.ID+"/reject", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", body)
	}

	stored, err := repo.GetVersion(context.Background(), 1, version.ID)
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeVersionPendingReview, stored.Status)

	body, _ := json.Marshal(map[string]string{"comment": "needs revision"})
	req := httptest.NewRequest(http.MethodPost, "/knowledge/doc-1/versions/"+version.ID+"/reject", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err = repo.GetVersion(context.Background(), 1, version.ID)
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeVersionRejected, stored.Status)
}

func TestListReviewTasksUsesReviewableKnowledgeBases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT NOT NULL, deleted_at DATETIME)`,
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_base_id TEXT NOT NULL, title TEXT NOT NULL, parse_status TEXT NOT NULL, pending_version_id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_versions (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, knowledge_id TEXT NOT NULL, version_label TEXT NOT NULL, content_hash TEXT NOT NULL, snapshot_ref TEXT, source_metadata TEXT NOT NULL DEFAULT '{}', previous_version_id TEXT, status TEXT NOT NULL, created_by TEXT NOT NULL, created_at DATETIME NOT NULL, effective_at DATETIME, expires_at DATETIME)`,
		`CREATE TABLE knowledge_version_reviews (id TEXT PRIMARY KEY, version_id TEXT NOT NULL, reviewer_id TEXT NOT NULL, action TEXT NOT NULL, comment TEXT, created_at DATETIME NOT NULL)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	repo := repository.NewKnowledgeGovernanceRepository(db, nil)
	submittedAt := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		kbID, kbName, knowledgeID, title, versionID string
		reviewerIDs                                 types.StringArray
	}{
		{"kb-visible", "Visible KB", "doc-visible", "Visible Doc", "version-visible", types.StringArray{"reviewer"}},
		{"kb-hidden", "Hidden KB", "doc-hidden", "Hidden Doc", "version-hidden", types.StringArray{"other-reviewer"}},
	} {
		require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id, name) VALUES (?, ?, ?)", row.kbID, 1, row.kbName).Error)
		require.NoError(t, db.Exec(
			"INSERT INTO knowledges (id, tenant_id, knowledge_base_id, title, parse_status, pending_version_id) VALUES (?, ?, ?, ?, ?, ?)",
			row.knowledgeID, 1, row.kbID, row.title, types.ParseStatusPendingReview, row.versionID,
		).Error)
		version := &types.KnowledgeVersion{
			ID: row.versionID, TenantID: 1, KnowledgeID: row.knowledgeID, VersionLabel: "v1",
			ContentHash: types.HashKnowledgeContent([]byte(row.versionID)),
			SourceMetadata: types.KnowledgeSourceMetadata{
				Layer: types.KnowledgeLayerFoundation, SourceCategory: "test", AuthorityLevel: "test",
			},
			Status: types.KnowledgeVersionPendingReview, CreatedBy: "author", CreatedAt: submittedAt,
		}
		require.NoError(t, repo.CreateVersion(context.Background(), version))
		require.NoError(t, repo.CreateReview(context.Background(), &types.KnowledgeVersionReview{
			ID: uuid.NewString(), VersionID: row.versionID, ReviewerID: "author", Action: "submit", CreatedAt: submittedAt,
		}))
	}

	h := NewKnowledgeGovernanceHandler(repo, nil, reviewTestKnowledgeBaseService{kbs: []*types.KnowledgeBase{
		{ID: "kb-visible", TenantID: 1, Governance: types.KnowledgeGovernanceConfig{Enabled: true, ProfileID: "p", ProfileVersion: "1"}, ReviewerIDs: types.StringArray{"reviewer"}},
		{ID: "kb-hidden", TenantID: 1, Governance: types.KnowledgeGovernanceConfig{Enabled: true, ProfileID: "p", ProfileVersion: "1"}, ReviewerIDs: types.StringArray{"other-reviewer"}},
	}})

	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.GET("/knowledge-versions/review-tasks", func(c *gin.Context) {
		withGovernanceReviewContext(c, 1, "reviewer")
		h.ListReviewTasks(c)
	})
	r.GET("/knowledge-versions/review-tasks/pending-count", func(c *gin.Context) {
		withGovernanceReviewContext(c, 1, "reviewer")
		h.PendingReviewCount(c)
	})

	listReq := httptest.NewRequest(http.MethodGet, "/knowledge-versions/review-tasks?status=pending", nil)
	listResp := httptest.NewRecorder()
	r.ServeHTTP(listResp, listReq)
	require.Equal(t, http.StatusOK, listResp.Code)

	var listPayload struct {
		Success bool                       `json:"success"`
		Data    []types.KnowledgeReviewTask `json:"data"`
		Total   int64                      `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listResp.Body.Bytes(), &listPayload))
	require.True(t, listPayload.Success)
	require.Equal(t, int64(1), listPayload.Total)
	require.Len(t, listPayload.Data, 1)
	require.Equal(t, "kb-visible", listPayload.Data[0].KnowledgeBaseID)
	require.Equal(t, "Visible Doc", listPayload.Data[0].KnowledgeTitle)

	countReq := httptest.NewRequest(http.MethodGet, "/knowledge-versions/review-tasks/pending-count", nil)
	countResp := httptest.NewRecorder()
	r.ServeHTTP(countResp, countReq)
	require.Equal(t, http.StatusOK, countResp.Code)

	var countPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(countResp.Body.Bytes(), &countPayload))
	require.Equal(t, int64(1), countPayload.Data.Count)
}
