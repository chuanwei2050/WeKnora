package web_search

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

// ValidateProxyURL delegates to utils.ValidateURLForSSRF (only http/https pass that check).
func ValidateProxyURL(proxyURL string) error {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	return utils.ValidateURLForSSRF(proxyURL)
}

func ssrfSafeRedirect(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if err := utils.ValidateURLForSSRF(req.URL.String()); err != nil {
			return fmt.Errorf("%w: %v", utils.ErrSSRFRedirectBlocked, err)
		}
		return nil
	}
}

// NewSearchHTTPClient builds an http.Client for outbound web search requests.
// It uses utils.SSRFSafeDialContext, optional explicit or environment proxy, and
// redirect validation consistent with utils.NewSSRFSafeHTTPClient.
func NewSearchHTTPClient(timeout time.Duration, proxyURL string) (*http.Client, error) {
	if strictAirGappedMode() {
		return nil, fmt.Errorf("strict air-gapped mode requires an approved search endpoint")
	}
	proxyURL = strings.TrimSpace(proxyURL)
	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not *http.Transport")
	}
	t := def.Clone()
	t.DialContext = utils.SSRFSafeDialContext

	if proxyURL != "" {
		if err := ValidateProxyURL(proxyURL); err != nil {
			return nil, err
		}
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy_url: %w", err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid proxy_url: scheme and host are required")
		}
		t.Proxy = http.ProxyURL(u)
	} else {
		t.Proxy = http.ProxyFromEnvironment
	}

	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	return &http.Client{
		Timeout:       timeout,
		Transport:     t,
		CheckRedirect: ssrfSafeRedirect(cfg.MaxRedirects),
	}, nil
}

// NewApprovedSearchHTTPClient creates a direct client for an administrator-
// approved private search endpoint. It validates the endpoint at construction,
// on every DNS resolution and on every redirect. A proxy is deliberately not
// accepted here because the approved endpoint must be the actual dial target.
func NewApprovedSearchHTTPClient(timeout time.Duration, endpoint *types.ApprovedEndpoint) (*http.Client, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("approved search endpoint is required")
	}
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if err := endpoint.ValidateUse(types.EndpointCategorySearch, "query"); err != nil {
		return nil, err
	}

	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not *http.Transport")
	}
	t := def.Clone()
	t.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		raw := fmt.Sprintf("%s://%s", endpoint.Scheme, net.JoinHostPort(host, port))
		if err := validateApprovedSearchTarget(ctx, endpoint, raw, ips); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, address)
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: t,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			if req == nil || req.URL == nil {
				return fmt.Errorf("redirect target is empty")
			}
			ips, err := net.DefaultResolver.LookupIP(req.Context(), "ip", req.URL.Hostname())
			if err != nil {
				return fmt.Errorf("resolve approved search redirect: %w", err)
			}
			if err := validateApprovedSearchTarget(req.Context(), endpoint, req.URL.String(), ips); err != nil {
				return fmt.Errorf("%w: %v", utils.ErrSSRFRedirectBlocked, err)
			}
			return nil
		},
	}, nil
}

func validateApprovedSearchTarget(ctx context.Context, endpoint *types.ApprovedEndpoint, raw string, ips []net.IP) error {
	if err := endpoint.ValidateConnection(raw, types.EndpointCategorySearch, "query", ips, strictAirGappedMode()); err != nil {
		return err
	}
	if strictAirGappedMode() {
		if err := endpoint.ValidateDeploymentAllowlist(utils.IsSSRFWhitelisted, ips, true); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func approvedSearchEndpointRoot(endpoint *types.ApprovedEndpoint) (string, error) {
	if endpoint == nil {
		return "", nil
	}
	if err := endpoint.Validate(); err != nil {
		return "", err
	}
	if err := endpoint.ValidateUse(types.EndpointCategorySearch, "query"); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s:%d", strings.ToLower(endpoint.Scheme), strings.ToLower(endpoint.Host), endpoint.Port), nil
}

func approvedSearchEndpointURL(endpoint *types.ApprovedEndpoint, suffix string) (string, error) {
	root, err := approvedSearchEndpointRoot(endpoint)
	if err != nil || root == "" {
		return root, err
	}
	return strings.TrimRight(root, "/") + "/" + strings.TrimLeft(suffix, "/"), nil
}

func newSearchHTTPClientForParameters(timeout time.Duration, params types.WebSearchProviderParameters) (*http.Client, error) {
	if params.ApprovedEndpoint != nil {
		if strings.TrimSpace(params.ProxyURL) != "" {
			return nil, fmt.Errorf("proxy_url cannot be combined with an approved search endpoint")
		}
		return NewApprovedSearchHTTPClient(timeout, params.ApprovedEndpoint)
	}
	return NewSearchHTTPClient(timeout, params.ProxyURL)
}

func strictAirGappedMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}
