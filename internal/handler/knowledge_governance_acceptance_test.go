//go:build acceptance

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openKnowledgeGovernanceAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE knowledge_versions (
			id TEXT PRIMARY KEY,
			tenant_id INTEGER NOT NULL,
			knowledge_id TEXT NOT NULL,
			version_label TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			snapshot_ref TEXT,
			previous_version_id TEXT,
			source_metadata TEXT NOT NULL DEFAULT '{}',
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
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestKnowledgeGovernanceAPIFormatsVersionHistoryAndCitation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openKnowledgeGovernanceAPITestDB(t)
	repo := repository.NewKnowledgeGovernanceRepository(db, nil)
	governance := NewKnowledgeGovernanceHandler(repo, nil, nil)

	effectiveAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	current := &types.KnowledgeVersion{
		ID: "version-current", TenantID: 9, KnowledgeID: "knowledge-api", VersionLabel: "2026.07",
		ContentHash: types.HashKnowledgeContent([]byte("current")), SnapshotRef: "snapshot://current",
		SourceMetadata: types.KnowledgeSourceMetadata{
			Layer: types.KnowledgeLayerStandard, SourceCategory: "national_standard", StandardNumber: "GB/T-1",
			VersionLabel: "2026", AuthorityLevel: "primary", EffectiveAt: &effectiveAt,
		},
		Status: types.KnowledgeVersionActive, CreatedBy: "author", CreatedAt: effectiveAt.Add(-time.Hour), EffectiveAt: &effectiveAt,
	}
	previous := &types.KnowledgeVersion{
		ID: "version-previous", TenantID: 9, KnowledgeID: "knowledge-api", VersionLabel: "2025.12",
		ContentHash: types.HashKnowledgeContent([]byte("previous")), SnapshotRef: "snapshot://previous",
		SourceMetadata: types.KnowledgeSourceMetadata{
			Layer: types.KnowledgeLayerStandard, SourceCategory: "national_standard", StandardNumber: "GB/T-1",
			VersionLabel: "2025", AuthorityLevel: "primary",
		},
		Status: types.KnowledgeVersionSuperseded, CreatedBy: "author", CreatedAt: effectiveAt.Add(-24 * time.Hour),
	}
	for _, version := range []*types.KnowledgeVersion{current, previous} {
		if err := repo.CreateVersion(t.Context(), version); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CreateReview(t.Context(), &types.KnowledgeVersionReview{
		ID: uuid.NewString(), VersionID: current.ID, ReviewerID: "reviewer", Action: "approve",
		Comment: "verified against the authoritative source", CreatedAt: effectiveAt.Add(-30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(9))
		c.Set(types.UserIDContextKey.String(), "reviewer")
		c.Next()
	})
	router.GET("/knowledge/:id/versions", governance.ListVersions)
	router.GET("/knowledge/:id/versions/:version_id", governance.GetVersion)

	listRequest := httptest.NewRequest(http.MethodGet, "/knowledge/knowledge-api/versions", nil)
	listResponse := httptest.NewRecorder()
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list versions status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var listPayload struct {
		Success bool                     `json:"success"`
		Data    []types.KnowledgeVersion `json:"data"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if !listPayload.Success || len(listPayload.Data) != 2 {
		t.Fatalf("version history = %+v, want two versions", listPayload)
	}
	if listPayload.Data[0].SourceMetadata.StandardNumber != "GB/T-1" || listPayload.Data[0].SourceMetadata.VersionLabel != "2026" {
		t.Fatalf("source metadata was not preserved: %+v", listPayload.Data[0].SourceMetadata)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/knowledge/knowledge-api/versions/version-current", nil)
	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("get version status = %d, body = %s", detailResponse.Code, detailResponse.Body.String())
	}
	var detailPayload struct {
		Success bool `json:"success"`
		Data    struct {
			Version types.KnowledgeVersion         `json:"version"`
			Reviews []types.KnowledgeVersionReview `json:"reviews"`
		} `json:"data"`
	}
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detailPayload); err != nil {
		t.Fatal(err)
	}
	if !detailPayload.Success || detailPayload.Data.Version.ID != current.ID || len(detailPayload.Data.Reviews) != 1 {
		t.Fatalf("version detail/history = %+v", detailPayload.Data)
	}

	// SearchResult is the citation payload consumed by the chat/search API.
	citation := types.SearchResult{
		KnowledgeID: current.KnowledgeID, KnowledgeVersionID: current.ID,
		KnowledgeLayer: current.SourceMetadata.Layer, SourceCategory: current.SourceMetadata.SourceCategory,
		EffectiveAt: current.EffectiveAt, KnowledgeFilename: "standard.pdf", StartAt: 12,
	}
	citationJSON, err := json.Marshal(citation)
	if err != nil {
		t.Fatal(err)
	}
	var citationPayload map[string]any
	if err := json.Unmarshal(citationJSON, &citationPayload); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"knowledge_id":         current.KnowledgeID,
		"knowledge_version_id": current.ID,
		"knowledge_layer":      string(current.SourceMetadata.Layer),
		"source_category":      current.SourceMetadata.SourceCategory,
		"knowledge_filename":   "standard.pdf",
	} {
		if got, ok := citationPayload[field].(string); !ok || got != want {
			t.Fatalf("citation field %q = %#v, want %q", field, citationPayload[field], want)
		}
	}
}
