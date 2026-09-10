package structuredquery

import (
	"context"
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
