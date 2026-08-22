package handler

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type integrationContextKey string

type batchSearchSessionService struct {
	interfaces.SessionService
	mu        sync.Mutex
	active    int
	maxActive int
}

func (s *batchSearchSessionService) SearchKnowledge(_ context.Context, knowledgeBaseIDs []string, knowledgeIDs []string, query string) ([]*types.SearchResult, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return []*types.SearchResult{{ID: "chunk-" + query, KnowledgeBaseID: knowledgeBaseIDs[0], KnowledgeID: strings.Join(knowledgeIDs, ","), Content: "evidence-" + query, Score: 0.9}}, nil
}

type batchSearchKnowledgeBaseService struct {
	interfaces.KnowledgeBaseService
}

func (batchSearchKnowledgeBaseService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{{ID: "kb-1", Name: "authorized"}}, nil
}

func TestIntegrationGenerationContextSurvivesRequestCancellation(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.WithValue(context.Background(), integrationContextKey("tenant"), "10000"))
	generationContext, cancelGeneration := newIntegrationGenerationContext(requestContext)
	cancelRequest()

	require.NoError(t, generationContext.Err())
	require.Equal(t, "10000", generationContext.Value(integrationContextKey("tenant")))

	cancelGeneration()
	require.ErrorIs(t, generationContext.Err(), context.Canceled)
}

func TestIntegrationReferencesAcceptsEventPayloadShapes(t *testing.T) {
	reference := &types.SearchResult{ID: "chunk-1", KnowledgeTitle: "测试文档"}

	for name, payload := range map[string]any{
		"references":     types.References{reference},
		"search results": []*types.SearchResult{reference},
		"interfaces":     []interface{}{reference},
	} {
		t.Run(name, func(t *testing.T) {
			references, ok := integrationReferences(payload)
			require.True(t, ok)
			require.Equal(t, types.References{reference}, references)
		})
	}
}

func TestIntegrationReferencesRejectsUnknownPayload(t *testing.T) {
	references, ok := integrationReferences([]interface{}{map[string]any{"id": "chunk-1"}})
	require.False(t, ok)
	require.Nil(t, references)
}

func TestIntegrationRateLimiterBoundsDistinctKeys(t *testing.T) {
	limiter := newIntegrationRateLimiter()
	for i := 0; i < 4096; i++ {
		require.True(t, limiter.allow(string(rune(i))+"-key", 1))
	}
	require.False(t, limiter.allow("overflow-key", 1))
}

func TestSupportedIntegrationImageValidatesTypeBase64AndSize(t *testing.T) {
	valid := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png"))
	require.True(t, isSupportedIntegrationImage(valid))
	require.False(t, isSupportedIntegrationImage("data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString([]byte("svg"))))
	require.False(t, isSupportedIntegrationImage("data:image/png;base64,not-base64"))
}

func TestIntegrationMessageStatusPreservesRunningAndCancelled(t *testing.T) {
	require.Equal(t, "running", integrationMessageStatus(false, nil))
	require.Equal(t, "completed", integrationMessageStatus(true, nil))
	events := []integrationauth.StreamEvent{{Event: "error", DataJSON: `{"status":"cancelled"}`, OccurredAt: time.Now()}}
	require.Equal(t, "cancelled", integrationMessageStatus(true, events))
}

func TestIntegrationRequestBodyLimit(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/api/integration/v1/auth/token", strings.NewReader(strings.Repeat("x", 32)))
	handler := &IntegrationHandler{limits: integrationLimits{maxRequestBytes: 16}}
	handler.limitRequestBody(context)
	_, err := io.ReadAll(context.Request.Body)
	require.Error(t, err)
}

func TestIntegrationKnowledgeIDsAreBoundedAndValidated(t *testing.T) {
	require.True(t, validIntegrationKnowledgeIDs([]string{"doc-1", "doc_2"}, 2))
	require.False(t, validIntegrationKnowledgeIDs([]string{"doc-1", "doc-2", "doc-3"}, 2))
	require.False(t, validIntegrationKnowledgeIDs([]string{"../foreign"}, 2))
	require.False(t, validIntegrationKnowledgeIDs([]string{strings.Repeat("x", 129)}, 2))
}

func TestIntegrationBatchSearchValidationRejectsDuplicateAndOversizedPlans(t *testing.T) {
	knowledgeBaseIDs := []string{"kb-1"}
	limits := integrationLimits{maxKnowledgeBases: 2, maxKnowledgeIDs: 2, maxBatchQueries: 2, maxQueryBytes: 20, maxTopK: 5}
	valid := integrationBatchSearchRequest{KnowledgeBaseIDs: &knowledgeBaseIDs, Queries: []integrationBatchSearchQuery{{ID: "q-1", Query: "first"}, {ID: "q-2", Query: "second"}}}
	require.Empty(t, validateIntegrationBatchSearchRequest(valid, limits))

	duplicate := valid
	duplicate.Queries[1].ID = "q-1"
	require.Equal(t, "duplicate_query_id", validateIntegrationBatchSearchRequest(duplicate, limits))

	whitespaceID := valid
	whitespaceID.Queries = []integrationBatchSearchQuery{{ID: " q-1", Query: "first"}}
	require.Equal(t, "limit_exceeded", validateIntegrationBatchSearchRequest(whitespaceID, limits))

	oversized := valid
	oversized.Queries = append(oversized.Queries, integrationBatchSearchQuery{ID: "q-3", Query: "third"})
	require.Equal(t, "limit_exceeded", validateIntegrationBatchSearchRequest(oversized, limits))
}

func TestIntegrationBatchSearchPreservesQueryOrderAndBoundsConcurrency(t *testing.T) {
	sessions := &batchSearchSessionService{}
	handler := &IntegrationHandler{
		sessions: sessions,
		kbs:      batchSearchKnowledgeBaseService{},
		limits:   integrationLimits{defaultTopK: 2, batchConcurrency: 2},
	}
	knowledgeIDs := []string{"doc-1"}
	queries := []integrationBatchSearchQuery{
		{ID: "q-1", Query: "first", KnowledgeIDs: &knowledgeIDs},
		{ID: "q-2", Query: "second"},
		{ID: "q-3", Query: "third"},
	}

	results, forbidden := handler.runIntegrationSearchBatch(context.Background(), []string{"kb-1"}, queries)

	require.False(t, forbidden)
	require.Equal(t, []string{"q-1", "q-2", "q-3"}, []string{results[0].ID, results[1].ID, results[2].ID})
	require.Equal(t, "chunk-first", results[0].Results[0]["chunk_id"])
	require.Equal(t, "authorized", results[0].Results[0]["knowledge_base_name"])
	require.Equal(t, 2, sessions.maxActive)
}

func TestIntegrationBrowserCookieCoversAPIAndFilesOnWeKnoraOrigin(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://weknora.example/api/integration/v1/auth/exchange", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	context.Request = request

	setIntegrationBrowserCookie(context, "session", 60)

	cookie := recorder.Result().Cookies()[0]
	require.Equal(t, integrationBrowserCookiePath, cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	weknoraOrigin, err := url.Parse("https://weknora.example/")
	require.NoError(t, err)
	jar.SetCookies(weknoraOrigin, []*http.Cookie{cookie})

	apiURL, err := url.Parse("https://weknora.example/api/v1/knowledge-bases")
	require.NoError(t, err)
	require.Len(t, jar.Cookies(apiURL), 1)
	fileURL, err := url.Parse("https://weknora.example/files?file_path=local%3A%2F%2Fdocument.pdf")
	require.NoError(t, err)
	require.Len(t, jar.Cookies(fileURL), 1)
	otherOrigin, err := url.Parse("https://host.example/api/v1/knowledge-bases")
	require.NoError(t, err)
	require.Empty(t, jar.Cookies(otherOrigin))
}

func TestIntegrationBrowserCookieAllowsHTTPWhenNotSecureContext(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "http://192.168.10.232:8089/api/integration/v1/auth/exchange", nil)

	setIntegrationBrowserCookie(context, "session", 60)

	cookie := recorder.Result().Cookies()[0]
	require.Equal(t, integrationBrowserCookiePath, cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.False(t, cookie.Secure)
}

func TestIntegrationBrowserSessionTokenPrefersBearerThenCookie(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet, "http://weknora.example/api/integration/v1/knowledge-bases", nil)
	context.Request.AddCookie(&http.Cookie{Name: integrationauth.BrowserCookieName, Value: "cookie-session"})
	context.Request.Header.Set("Authorization", "Bearer bearer-session")
	require.Equal(t, "bearer-session", integrationBrowserSessionToken(context))

	context.Request = httptest.NewRequest(http.MethodGet, "http://weknora.example/api/integration/v1/knowledge-bases", nil)
	context.Request.AddCookie(&http.Cookie{Name: integrationauth.BrowserCookieName, Value: "cookie-only"})
	require.Equal(t, "cookie-only", integrationBrowserSessionToken(context))

	context.Request = httptest.NewRequest(http.MethodGet, "http://weknora.example/api/integration/v1/knowledge-bases", nil)
	context.Request.Header.Set("Authorization", "Bearer bearer-only")
	require.Equal(t, "bearer-only", integrationBrowserSessionToken(context))
}

func TestIntegrationChatSessionResponseKeepsPublicModeAndScope(t *testing.T) {
	response := integrationChatSessionResponse(&types.Session{ID: "session", Title: "title"}, "selected", []string{"kb-1"})

	require.Equal(t, "session", response["id"])
	require.Equal(t, "title", response["title"])
	require.Equal(t, "selected", response["knowledge_base_mode"])
	require.Equal(t, []string{"kb-1"}, response["allowed_knowledge_base_ids"])
}
