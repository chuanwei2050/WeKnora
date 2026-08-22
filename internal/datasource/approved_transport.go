package datasource

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	modeltransport "github.com/Tencent/WeKnora/internal/models/transport"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// NewApprovedEndpointHTTPClient binds a connector client to the approved
// endpoint's scheme, host and port. The transport validates the resolved IP
// for every dial and rejects redirects to a different destination.
func NewApprovedEndpointHTTPClient(endpoint *types.ApprovedEndpoint, timeout time.Duration) (*http.Client, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("approved endpoint is required")
	}
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if err := endpoint.ValidateUse(types.EndpointCategoryDataConnector, "sync"); err != nil {
		return nil, err
	}
	baseURL := fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port)
	return modeltransport.NewEndpointHTTPClientWithValidation(baseURL, timeout, func(ip net.IP) error {
		if err := endpoint.ValidateConnection(baseURL, types.EndpointCategoryDataConnector, "sync", []net.IP{ip}, strictAirGappedMode()); err != nil {
			return err
		}
		if strictAirGappedMode() {
			if err := endpoint.ValidateDeploymentAllowlist(secutils.IsSSRFWhitelisted, []net.IP{ip}, true); err != nil {
				return err
			}
		}
		return nil
	})
}

func strictAirGappedMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}
