package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestStripStructuredMetadataRemovesBindings(t *testing.T) {
	knowledge := &types.Knowledge{
		Metadata: types.JSON(`{"structured_dataset_id":"d1","structured_version_id":"v1","structured_job_id":"j1","structured_submitted_at":"1","keep":"yes"}`),
	}
	stripStructuredMetadata(knowledge)
	meta := knowledge.GetMetadata()
	require.Equal(t, "yes", meta["keep"])
	require.Empty(t, meta["structured_dataset_id"])
	require.Empty(t, meta["structured_version_id"])
	require.Empty(t, meta["structured_job_id"])
	require.Empty(t, meta["structured_submitted_at"])
}

func TestCleanupStructuredDatasetsDeletesByPrefixAndClearsMetadata(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/v1/datasets", r.URL.Path)
		require.Equal(t, "kb-1", r.URL.Query().Get("namespace"))
		require.Equal(t, "knowledge-1-", r.URL.Query().Get("idempotency_prefix"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":2}`))
	}))
	defer server.Close()

	svc := &knowledgeService{config: &config.Config{StructuredQuery: &config.StructuredQueryConfig{
		Enabled: true, BaseURL: server.URL, APIKey: "key", RequestTimeout: 2,
	}}}
	knowledge := &types.Knowledge{
		ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1",
		Metadata: types.JSON(`{"structured_dataset_id":"dataset-1","keep":"x"}`),
	}
	svc.cleanupStructuredDatasets(context.Background(), knowledge)
	require.True(t, called)
	require.Empty(t, knowledge.GetMetadata()["structured_dataset_id"])
	require.Equal(t, "x", knowledge.GetMetadata()["keep"])
}

func TestResubmitStructuredFileDeletesOldDatasetBeforeUpload(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodDelete:
			require.Equal(t, "/v1/datasets", r.URL.Path)
			require.Equal(t, "kb-1", r.URL.Query().Get("namespace"))
			require.Equal(t, "knowledge-1-", r.URL.Query().Get("idempotency_prefix"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":1}`))
		case http.MethodPost:
			require.Equal(t, "/v1/datasets/files", r.URL.Path)
			require.Equal(t, "knowledge-1-hash-1-run-1", r.Header.Get("Idempotency-Key"))
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"dataset_id":"dataset-2","version_id":"version-2","job_id":"job-2"}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	cfg := &config.StructuredQueryConfig{Enabled: true, BaseURL: server.URL, APIKey: "key", RequestTimeout: 2}
	svc := &knowledgeService{config: &config.Config{StructuredQuery: cfg}}
	merger := &structuredQueryTestMerger{}
	knowledge := &types.Knowledge{
		ID: "knowledge-1", TenantID: 7, KnowledgeBaseID: "kb-1",
		FileName: "people.csv", FileHash: "hash-1", FilePath: "local://people.csv",
		Metadata: types.JSON(`{"structured_dataset_id":"dataset-1"}`),
	}

	accepted, err := svc.resubmitStructuredFile(
		context.Background(),
		cfg,
		knowledge,
		structuredQueryTestFiles{content: "name\nAlice\n"},
		merger,
		"knowledge-1-hash-1-run-1",
	)
	require.NoError(t, err)
	require.Equal(t, []string{"DELETE /v1/datasets", "POST /v1/datasets/files"}, methods)
	require.Equal(t, "dataset-2", accepted.DatasetID)
	require.Equal(t, "dataset-2", merger.metadata["structured_dataset_id"])
}

func TestQueueStructuredResubmitSkipsNonTableFiles(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	svc := &knowledgeService{config: &config.Config{StructuredQuery: &config.StructuredQueryConfig{
		Enabled: true, BaseURL: server.URL, APIKey: "key", RequestTimeout: 2,
	}}}
	svc.queueStructuredResubmit(context.Background(), &types.Knowledge{
		ID: "knowledge-1", FileName: "notes.pdf", FileHash: "hash-1",
	}, structuredQueryTestFiles{content: "x"}, "unused")
	time.Sleep(50 * time.Millisecond)
	require.False(t, called)
}
