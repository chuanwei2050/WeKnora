package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSanitizeBodyRedactsIntegrationCredentials(t *testing.T) {
	input := `{"client_secret":"client-value","ticket":"ticket-value","csrf_token":"csrf-value","access_token":"access-value"}`
	result := sanitizeBody(input)
	for _, secret := range []string{"client-value", "ticket-value", "csrf-value", "access-value"} {
		if strings.Contains(result, secret) {
			t.Fatalf("credential %q was not redacted: %s", secret, result)
		}
	}
}

func TestReadRequestBodyTruncatesLogWithoutTruncatingHandlerBody(t *testing.T) {
	body := `{"query":"` + strings.Repeat("x", maxBodySize*2) + `"}`
	request := httptest.NewRequest("POST", "/api/integration/v1/rag/search", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	logged := readRequestBody(context)
	if !strings.Contains(logged, "内容过长") {
		t.Fatalf("expected truncated log marker")
	}
	replayed, err := io.ReadAll(context.Request.Body)
	if err != nil || string(replayed) != body {
		t.Fatalf("handler body changed: err=%v got=%d want=%d", err, len(replayed), len(body))
	}
}
