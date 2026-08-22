package datasource

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNewApprovedEndpointHTTPClientRejectsRedirectToDifferentPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "http://127.0.0.1:1/private")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	endpointURL := server.URL
	endpoint := &types.ApprovedEndpoint{
		TenantID:    1,
		Scheme:      "http",
		Host:        "127.0.0.1",
		Port:        server.Listener.Addr().(*net.TCPAddr).Port,
		Category:    types.EndpointCategoryDataConnector,
		AllowedUses: types.StringArray{"sync"},
	}
	client, err := NewApprovedEndpointHTTPClient(endpoint, 2*time.Second)
	if err != nil {
		t.Fatalf("create approved client: %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, endpointURL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected redirect to a different port to be rejected")
	}
}
