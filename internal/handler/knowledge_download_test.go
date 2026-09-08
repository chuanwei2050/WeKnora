package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type readSeekCloser struct{ *bytes.Reader }

func (readSeekCloser) Close() error { return nil }

func TestServeKnowledgeDownloadSupportsRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	ctx.Request.Header.Set("Range", "bytes=2-5")
	file := readSeekCloser{Reader: bytes.NewReader([]byte("0123456789"))}

	serveKnowledgeDownload(ctx, file, "测试.docx", 10, time.Unix(1, 0))

	result := recorder.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, result.StatusCode)
	require.Equal(t, "bytes 2-5/10", result.Header.Get("Content-Range"))
	require.Equal(t, "4", result.Header.Get("Content-Length"))
	require.Equal(t, "2345", string(body))
}
