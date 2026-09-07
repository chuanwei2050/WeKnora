package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type readKnowledgeService struct {
	folderEndpointKnowledgeService
	knowledge *types.Knowledge
	chunks    []*types.Chunk
	reads     int
}

func (s *readKnowledgeService) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return s.knowledge, nil
}
func (s *readKnowledgeService) ReadIntegrationKnowledgeChunks(_ context.Context, tenant uint64, id, version string, page, size int) ([]*types.Chunk, int64, error) {
	s.reads++
	return s.chunks, 2, nil
}

func TestIntegrationKnowledgeReadScopeRevisionAndExactText(t *testing.T) {
	for _, scenario := range []struct {
		name                string
		tenant              uint64
		kb, folder, enabled string
		stale               bool
		status              int
	}{
		{"valid", 1, "kb-1", "folder", "enabled", false, 200},
		{"unknown name remains valid", 1, "kb-1", "folder", "enabled", false, 200},
		{"different tenant", 2, "kb-1", "folder", "enabled", false, 404},
		{"different knowledge base", 1, "other", "folder", "enabled", false, 404},
		{"disabled folder", 1, "kb-1", "closed", "enabled", false, 403},
		{"disabled document", 1, "kb-1", "folder", "disabled", false, 403},
		{"stale revision", 1, "kb-1", "folder", "enabled", true, 409},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, _ := newFolderEndpointHandler(t, nil, nil)
			revision := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			actual := revision
			if scenario.stale {
				actual = actual.Add(time.Second)
			}
			service := &readKnowledgeService{folderEndpointKnowledgeService: folderEndpointKnowledgeService{searchableIDs: []string{"folder"}},
				knowledge: &types.Knowledge{ID: "doc", TenantID: scenario.tenant, KnowledgeBaseID: scenario.kb, TagID: scenario.folder,
					EnableStatus: scenario.enabled, UpdatedAt: actual, Title: scenario.name},
				chunks: []*types.Chunk{{ID: "chunk", TenantID: 1, KnowledgeID: "doc", KnowledgeBaseID: "kb-1", Content: "  原文\nαβ  ", IsEnabled: true}},
			}
			h.knowledges = service
			principal := &integrationauth.Principal{ClientID: "client", TenantID: 1, KnowledgeBaseIDs: []string{"kb-1"}, Scopes: []string{"knowledge:read"}}
			ctx, recorder := folderEndpointContext(http.MethodPost, "/knowledge/read", "", principal)
			body, _ := json.Marshal(map[string]any{"knowledge_base_id": "kb-1", "knowledge_id": "doc", "updated_at": revision, "page": 1, "page_size": 1})
			ctx.Request.Body = io.NopCloser(strings.NewReader(string(body)))
			ctx.Request.Header.Set("Content-Type", "application/json")
			h.ReadKnowledgeChunks(ctx)
			require.Equal(t, scenario.status, recorder.Code, recorder.Body.String())
			if scenario.status == 200 {
				require.Contains(t, recorder.Body.String(), `"content":"  原文\nαβ  "`)
				require.Contains(t, recorder.Body.String(), `"next_page":2`)
				require.Equal(t, 1, service.reads)
			} else {
				require.Equal(t, 0, service.reads)
				require.NotContains(t, recorder.Body.String(), "原文")
			}
		})
	}
}
