package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type structuredQueryTestFiles struct{ content string }

func (f structuredQueryTestFiles) GetFile(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.content)), nil
}

type structuredQueryTestMerger struct{ metadata map[string]interface{} }

func (m *structuredQueryTestMerger) MergeKnowledgeMetadata(_ context.Context, _ uint64, _ string, value types.JSON) error {
	var err error
	m.metadata, err = value.Map()
	return err
}

func TestSubmitStructuredFileRequestUploadsAndPersistsIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "service-key", r.Header.Get("X-API-Key"))
		require.Equal(t, "7", r.Header.Get("X-Tenant-ID"))
		require.Equal(t, "knowledge-1-hash-1", r.Header.Get("Idempotency-Key"))
		require.NoError(t, r.ParseMultipartForm(1<<20))
		require.Equal(t, "kb-1", r.FormValue("namespace"))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		require.Equal(t, "people.csv", header.Filename)
		body, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, "name\nAlice\n", string(body))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"dataset_id":"dataset-1","version_id":"version-1","job_id":"job-1"}`))
	}))
	defer server.Close()

	merger := &structuredQueryTestMerger{}
	accepted, err := submitStructuredFileRequest(
		context.Background(),
		&config.StructuredQueryConfig{Enabled: true, BaseURL: server.URL, APIKey: "service-key", RequestTimeout: 2},
		&types.Knowledge{ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1", FileName: "people.csv", FileHash: "hash-1", FilePath: "local://people.csv"},
		structuredQueryTestFiles{content: "name\nAlice\n"},
		merger,
	)
	require.NoError(t, err)
	require.Equal(t, "job-1", accepted.JobID)
	require.Equal(t, "dataset-1", merger.metadata["structured_dataset_id"])
	require.Equal(t, "version-1", merger.metadata["structured_version_id"])
	require.Equal(t, "job-1", merger.metadata["structured_job_id"])
}

func TestSubmitStructuredFileRequestReportsRejectedUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := submitStructuredFileRequest(
		context.Background(),
		&config.StructuredQueryConfig{Enabled: true, BaseURL: server.URL, RequestTimeout: 2},
		&types.Knowledge{ID: "knowledge-1", KnowledgeBaseID: "kb-1", FileName: "people.csv"},
		structuredQueryTestFiles{content: "name\n"},
		&structuredQueryTestMerger{},
	)
	require.ErrorContains(t, err, "upload status=503")
}
