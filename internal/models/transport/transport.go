package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Timeout    time.Duration
	TLSConfig  *tls.Config
	Headers    map[string]string
	ValidateIP func(net.IP) error
	// ValidateURL is called for the initial request and every redirect. It is
	// the integration point for tenant-scoped approved endpoint registries.
	ValidateURL  func(*url.URL) error
	AllowedHosts []string
}

type OutboundAuditEvent struct {
	At         time.Time
	URL        string
	Allowed    bool
	StatusCode int
	Error      string
}

var outboundAudit struct {
	sync.RWMutex
	hook func(OutboundAuditEvent)
}

// SetOutboundAuditHook installs a process-local, redacted outbound audit hook.
// Passing nil disables it. The hook is intended for deployment audits and
// must not receive credentials or query parameters.
func SetOutboundAuditHook(hook func(OutboundAuditEvent)) {
	outboundAudit.Lock()
	outboundAudit.hook = hook
	outboundAudit.Unlock()
}

func emitOutboundAudit(event OutboundAuditEvent) {
	outboundAudit.RLock()
	hook := outboundAudit.hook
	outboundAudit.RUnlock()
	if hook != nil {
		hook(event)
	}
}

func auditURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	copy := *value
	copy.RawQuery = ""
	copy.Fragment = ""
	copy.User = nil
	return copy.String()
}

// NewHTTPClient centralizes cancellation, connection pooling, TLS and
// connection-time IP validation for model and connector adapters.
func NewHTTPClient(config Config) *http.Client {
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Minute
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	validateResolvedIPs := func(ctx context.Context, host string) error {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return err
		}
		if config.ValidateIP != nil {
			for _, ip := range ips {
				if err := config.ValidateIP(ip); err != nil {
					return err
				}
			}
		} else if airGappedTransport() {
			for _, ip := range ips {
				if !isPrivateNetworkIP(ip) {
					return fmt.Errorf("strict air-gapped transport rejected public IP %s", ip.String())
				}
			}
		}
		return nil
	}
	transport := &http.Transport{TLSClientConfig: config.TLSConfig, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if err := validateResolvedIPs(ctx, host); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, address)
	}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: config.Timeout, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 8}
	allowedHosts := make(map[string]bool, len(config.AllowedHosts))
	for _, host := range config.AllowedHosts {
		allowedHosts[strings.ToLower(strings.TrimSpace(host))] = true
	}
	client := &http.Client{Transport: roundTripFunc{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			if req == nil || req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				err := fmt.Errorf("request target has unsupported scheme")
				var target *url.URL
				if req != nil {
					target = req.URL
				}
				emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(target), Error: err.Error()})
				return nil, err
			}
			if len(allowedHosts) > 0 && !allowedHosts[strings.ToLower(req.URL.Hostname())] {
				err := fmt.Errorf("request target host is not approved")
				emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
				return nil, err
			}
			if config.ValidateURL != nil {
				if err := config.ValidateURL(req.URL); err != nil {
					err = fmt.Errorf("request target is not approved: %w", err)
					emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
					return nil, err
				}
			}
			response, err := transport.RoundTrip(req)
			event := OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Allowed: err == nil, Error: ""}
			if err != nil {
				event.Error = err.Error()
			}
			if response != nil {
				event.StatusCode = response.StatusCode
			}
			emitOutboundAudit(event)
			return response, err
		},
		closeIdle: transport.CloseIdleConnections,
	}, Timeout: config.Timeout, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			err := fmt.Errorf("redirect target has unsupported scheme")
			emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
			return err
		}
		if len(allowedHosts) > 0 && !allowedHosts[strings.ToLower(req.URL.Hostname())] {
			err := fmt.Errorf("redirect target host is not approved")
			emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
			return err
		}
		if config.ValidateURL != nil {
			if err := config.ValidateURL(req.URL); err != nil {
				err = fmt.Errorf("redirect target is not approved: %w", err)
				emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
				return err
			}
		}
		if err := validateResolvedIPs(req.Context(), req.URL.Hostname()); err != nil {
			emitOutboundAudit(OutboundAuditEvent{At: time.Now().UTC(), URL: auditURL(req.URL), Error: err.Error()})
			return err
		}
		return nil
	}}
	return client
}

func airGappedTransport() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}

func isPrivateNetworkIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

type roundTripFunc struct {
	roundTrip func(*http.Request) (*http.Response, error)
	closeIdle func()
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.roundTrip(req)
}

func (f roundTripFunc) CloseIdleConnections() {
	if f.closeIdle != nil {
		f.closeIdle()
	}
}

func NewEndpointHTTPClient(rawURL string, timeout time.Duration) (*http.Client, error) {
	return NewEndpointHTTPClientWithValidation(rawURL, timeout, nil)
}

// NewEndpointHTTPClientWithValidation constrains scheme/host/port and applies
// connection-time IP validation to the initial request and every redirect.
func NewEndpointHTTPClientWithValidation(rawURL string, timeout time.Duration, validateIP func(net.IP) error) (*http.Client, error) {
	if err := ValidateHTTPURL(rawURL); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	baseScheme, baseHost, basePort := strings.ToLower(parsed.Scheme), strings.ToLower(parsed.Hostname()), parsed.Port()
	if basePort == "" {
		if baseScheme == "https" {
			basePort = "443"
		} else {
			basePort = "80"
		}
	}
	return NewHTTPClient(Config{
		Timeout:      timeout,
		ValidateIP:   validateIP,
		AllowedHosts: []string{baseHost},
		ValidateURL: func(target *url.URL) error {
			targetPort := target.Port()
			if targetPort == "" {
				if strings.EqualFold(target.Scheme, "https") {
					targetPort = "443"
				} else {
					targetPort = "80"
				}
			}
			if !strings.EqualFold(target.Scheme, baseScheme) || !strings.EqualFold(target.Hostname(), baseHost) || targetPort != basePort {
				return fmt.Errorf("scheme, host or port changed")
			}
			return nil
		},
	}), nil
}

func ApplyHeaders(request *http.Request, headers map[string]string) {
	for key, value := range headers {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "api-key" || lower == "content-type" || lower == "host" {
			continue
		}
		request.Header.Set(key, value)
	}
}

func ValidateHTTPURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https are supported")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("host is required")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("credentials and URL fragments are not supported")
	}
	return nil
}
