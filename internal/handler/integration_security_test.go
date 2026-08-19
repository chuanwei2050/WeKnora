package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	integrationauth "github.com/Tencent/WeKnora/internal/integration"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntegrationRateLimiterBoundsDistinctKeys(t *testing.T) {
	limiter := newIntegrationRateLimiter()
	for i := 0; i < 4096; i++ {
		require.True(t, limiter.allow(string(rune(i))+"-key", 1))
	}
	require.False(t, limiter.allow("overflow-key", 1))
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
