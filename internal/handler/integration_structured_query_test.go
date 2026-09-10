package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/structuredquery"
	"github.com/Tencent/WeKnora/internal/types"
)

func TestRunIntegrationStructuredQueryUsesServiceResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant-ID") != "9" {
			t.Fatalf("unexpected tenant: %s", r.Header.Get("X-Tenant-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"route":"sql","sql":"SELECT name FROM people","columns":["name","note"],"rows":[["Alice",null],["Bob","ok"]]}`))
	}))
	defer server.Close()

	result := runIntegrationStructuredQuery(context.Background(), structuredquery.Client{BaseURL: server.URL, APIKey: "key"}, &types.Knowledge{TenantID: 9, KnowledgeBaseID: "kb"}, "dataset", integrationTableAnalysisQuery{ID: "q1", Query: "who"}, 1)
	if result.Status != "completed" || result.SQL == "" || result.RowCount != 1 || result.Rows[0]["name"] != "Alice" || result.Rows[0]["note"] != "NULL" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunIntegrationStructuredQueryPreservesNoneRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"route":"none"}`))
	}))
	defer server.Close()
	result := runIntegrationStructuredQuery(context.Background(), structuredquery.Client{BaseURL: server.URL, APIKey: "key"}, &types.Knowledge{TenantID: 9, KnowledgeBaseID: "kb"}, "dataset", integrationTableAnalysisQuery{ID: "q1", Query: "not tabular"}, 10)
	if result.Status != "skipped" || result.Error != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
