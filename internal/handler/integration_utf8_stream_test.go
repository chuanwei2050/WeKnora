package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIntegrationUTF8StreamBuffersSplitRunes(t *testing.T) {
	input := []byte("你好，流式回答")
	var stream integrationUTF8Stream
	var got string

	for _, b := range input {
		got += stream.Push(string([]byte{b}))
	}

	if got != string(input) {
		t.Fatalf("Push() = %q, want %q", got, string(input))
	}
	if len(stream.pending) != 0 {
		t.Fatalf("pending bytes = %d, want 0", len(stream.pending))
	}
}

func TestWriteIntegrationSSEErrorSendsTerminalEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeIntegrationSSEError(context, "session-1", "message-1", "stream_event_persist_failed")

	body := recorder.Body.String()
	for _, expected := range []string{`event: error`, `"event":"error"`, `"message_id":"message-1"`, `"status":"failed"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("SSE body %q does not contain %q", body, expected)
		}
	}
}
