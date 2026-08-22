package file

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApprovedStorageHTTPClientRejectsRedirectToDifferentPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://127.0.0.1:1/private", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	endpoint := &types.ApprovedEndpoint{
		TenantID:    1,
		Scheme:      "http",
		Host:        "127.0.0.1",
		Port:        port,
		Category:    types.EndpointCategoryObjectStorage,
		AllowedUses: types.StringArray{"object-storage"},
	}
	client, err := newApprovedStorageHTTPClient(endpoint)
	if err != nil {
		t.Fatalf("create approved storage client: %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected redirect to a different port to be rejected")
	}
}

func TestNormalizeMinioClientEndpointUsesApprovedSchemeAndHost(t *testing.T) {
	endpoint, useSSL := normalizeMinioClientEndpoint("minio.internal:9000", &types.ApprovedEndpoint{
		Scheme: "https",
		Host:   "minio.internal",
		Port:   9443,
	}, false)
	if endpoint != "minio.internal:9443" || !useSSL {
		t.Fatalf("normalized MinIO endpoint = %q, useSSL = %v", endpoint, useSSL)
	}
}
