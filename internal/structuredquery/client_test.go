package structuredquery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientQuerySendsTenantAndDatasetScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/query" || r.Header.Get("X-API-Key") != "secret" || r.Header.Get("X-Tenant-ID") != "7" {
			t.Fatalf("unexpected request: path=%s key=%s tenant=%s", r.URL.Path, r.Header.Get("X-API-Key"), r.Header.Get("X-Tenant-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"route":"sql","sql":"SELECT 1","columns":["n"],"rows":[[1]],"model_calls":1,"timings":{"total_ms":5},"sources":[]}`))
	}))
	defer server.Close()

	response, err := (Client{BaseURL: server.URL, APIKey: "secret", Timeout: time.Second}).Query(context.Background(), 7, Request{Namespace: "kb", Question: "count", DatasetIDs: []string{"dataset"}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Route != "sql" || response.SQL != "SELECT 1" || len(response.Rows) != 1 {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestClientQueryRejectsUnexpectedRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"route":"legacy"}`))
	}))
	defer server.Close()
	_, err := (Client{BaseURL: server.URL, APIKey: "secret"}).Query(context.Background(), 1, Request{Namespace: "kb", Question: "q"})
	if err == nil {
		t.Fatal("expected unexpected route error")
	}
}

func TestClientQueryRetriesModelUnavailableOnce(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"detail":"model_unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"route":"sql","sql":"SELECT 1","columns":["n"],"rows":[[1]],"model_calls":1,"timings":{},"sources":[]}`))
	}))
	defer server.Close()

	response, err := (Client{BaseURL: server.URL, APIKey: "secret", Timeout: time.Second}).Query(
		context.Background(), 1, Request{Namespace: "kb", Question: "count"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || response.Route != "sql" {
		t.Fatalf("attempts=%d response=%#v", attempts, response)
	}
}

func TestClientDeleteDatasetsByPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/datasets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("namespace") != "kb" || r.URL.Query().Get("idempotency_prefix") != "doc-1-" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		if r.Header.Get("X-API-Key") != "secret" || r.Header.Get("X-Tenant-ID") != "7" {
			t.Fatalf("missing auth headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":2}`))
	}))
	defer server.Close()

	deleted, err := (Client{BaseURL: server.URL, APIKey: "secret", Timeout: time.Second}).
		DeleteDatasetsByPrefix(context.Background(), 7, "kb", "doc-1-")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d", deleted)
	}
}

func TestShouldRetryStructuredQueryStatus(t *testing.T) {
	if !shouldRetryStructuredQueryStatus(http.StatusUnprocessableEntity, `{"detail":"model_unavailable"}`) {
		t.Fatal("model_unavailable should retry")
	}
	if shouldRetryStructuredQueryStatus(http.StatusUnprocessableEntity, `{"detail":"unsafe_sql"}`) {
		t.Fatal("non-model 422 must not retry")
	}
}

func TestIsSoftFailure(t *testing.T) {
	if !IsSoftFailure(fmt.Errorf("status 422: no_active_dataset")) {
		t.Fatal("no_active_dataset should be soft")
	}
	if !IsSoftFailure(fmt.Errorf("status 422: {\"detail\":\"no_relevant_table\"}")) {
		t.Fatal("no_relevant_table should be soft")
	}
	if IsSoftFailure(fmt.Errorf("status 422: unsupported_value_literal")) {
		t.Fatal("literal failures must stay hard")
	}
}
