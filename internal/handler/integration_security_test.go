package handler

import (
	"context"
	"encoding/base64"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type integrationContextKey string

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

func TestIntegrationBrowserCookieIsLimitedToKnowledgePath(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	setIntegrationBrowserCookie(context, "session", 60)

	cookie := recorder.Result().Cookies()[0]
	require.Equal(t, integrationBrowserCookiePath, cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
}

func TestIntegrationChatSessionResponseKeepsPublicModeAndScope(t *testing.T) {
	response := integrationChatSessionResponse(&types.Session{ID: "session", Title: "title"}, "selected", []string{"kb-1"})

	require.Equal(t, "session", response["id"])
	require.Equal(t, "title", response["title"])
	require.Equal(t, "selected", response["knowledge_base_mode"])
	require.Equal(t, []string{"kb-1"}, response["allowed_knowledge_base_ids"])
}
