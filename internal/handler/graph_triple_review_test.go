package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var errTripleNotPending = errors.New("not pending")

type memoryTripleRepo struct {
	items map[string]*types.GraphTripleCandidate
}

func (m *memoryTripleRepo) Enqueue(_ context.Context, c *types.GraphTripleCandidate) error {
	if m.items == nil {
		m.items = map[string]*types.GraphTripleCandidate{}
	}
	cp := *c
	m.items[c.ID] = &cp
	return nil
}
func (m *memoryTripleRepo) GetByID(_ context.Context, tenantID uint64, id string) (*types.GraphTripleCandidate, error) {
	item := m.items[id]
	if item == nil || item.TenantID != tenantID {
		return nil, nil
	}
	cp := *item
	return &cp, nil
}
func (m *memoryTripleRepo) List(_ context.Context, tenantID uint64, knowledgeBaseID string, status types.GraphTripleReviewStatus) ([]*types.GraphTripleCandidate, error) {
	out := []*types.GraphTripleCandidate{}
	for _, item := range m.items {
		if item.TenantID != tenantID {
			continue
		}
		if knowledgeBaseID != "" && item.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		if status != "" && item.Status != status {
			continue
		}
		cp := *item
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memoryTripleRepo) SupersedePendingByKnowledgeBase(_ context.Context, tenantID uint64, knowledgeBaseID string) error {
	return nil
}
func (m *memoryTripleRepo) SupersedePendingByKnowledgeIDs(_ context.Context, tenantID uint64, knowledgeIDs []string) error {
	return nil
}
func (m *memoryTripleRepo) MarkSuperseded(_ context.Context, tenantID uint64, id string) error {
	return nil
}
func (m *memoryTripleRepo) MarkWritten(_ context.Context, tenantID uint64, id, reviewerID string) error {
	item := m.items[id]
	if item == nil || item.TenantID != tenantID || item.Status != types.GraphTriplePending {
		return errTripleNotPending
	}
	now := time.Now().UTC()
	item.Status = types.GraphTripleWritten
	item.ReviewerID = reviewerID
	item.ReviewedAt = &now
	item.WrittenAt = &now
	return nil
}
func (m *memoryTripleRepo) MarkRejected(_ context.Context, tenantID uint64, id, reviewerID, comment string) error {
	item := m.items[id]
	if item == nil || item.TenantID != tenantID || item.Status != types.GraphTriplePending {
		return errTripleNotPending
	}
	now := time.Now().UTC()
	item.Status = types.GraphTripleRejected
	item.ReviewerID = reviewerID
	item.Comment = comment
	item.ReviewedAt = &now
	return nil
}

func TestGraphTripleReviewRejectPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &memoryTripleRepo{items: map[string]*types.GraphTripleCandidate{
		"c1": {
			ID: "c1", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "k", ChunkID: "ch",
			Status: types.GraphTriplePending, GraphData: types.GraphDataPayload{Relation: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "uses"}}},
		},
	}}
	h := NewGraphTripleReviewHandler(repo, nil, nil, nil)
	r := gin.New()
	r.POST("/graph-triple-reviews/:id/reject", func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "u1")
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserContextKey, &types.User{
			ID: "u1", BidReviewRole: "",
		}))
		h.Reject(c)
	})
	body, _ := json.Marshal(map[string]string{"comment": "noise"})
	req := httptest.NewRequest(http.MethodPost, "/graph-triple-reviews/c1/reject", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, types.GraphTripleRejected, repo.items["c1"].Status)
	require.Equal(t, "noise", repo.items["c1"].Comment)
}

func TestCanAccessGraphTripleReviewNativeAndBidReview(t *testing.T) {
	native := context.WithValue(context.Background(), types.UserContextKey, &types.User{ID: "n1", BidReviewRole: ""})
	require.True(t, canAccessGraphTripleReview(native))
	member := context.WithValue(context.Background(), types.UserContextKey, &types.User{ID: "m1", BidReviewRole: "member"})
	require.False(t, canAccessGraphTripleReview(member))
	admin := context.WithValue(context.Background(), types.UserContextKey, &types.User{ID: "a1", BidReviewRole: "tenant_admin"})
	require.True(t, canAccessGraphTripleReview(admin))
}
