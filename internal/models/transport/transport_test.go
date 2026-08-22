package transport

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewHTTPClientRejectsUnapprovedInitialHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := NewHTTPClient(Config{AllowedHosts: []string{"different.invalid"}})
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected unapproved host to be rejected before dialing")
	}
}

func TestNewHTTPClientEmitsRedactedOutboundAudit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	var events []OutboundAuditEvent
	SetOutboundAuditHook(func(event OutboundAuditEvent) { events = append(events, event) })
	defer SetOutboundAuditHook(nil)
	client := NewHTTPClient(Config{AllowedHosts: []string{"127.0.0.1"}})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/audit?secret=redact", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !events[0].Allowed || events[0].StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected audit events: %+v", events)
	}
	if strings.Contains(events[0].URL, "secret=redact") {
		t.Fatal("audit event leaked query parameters")
	}
}

func TestNewHTTPClientValidatesInitialAndRedirectURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := NewHTTPClient(Config{
		AllowedHosts: []string{parsed.Hostname()},
		ValidateURL: func(target *url.URL) error {
			if target.Path == "/target" {
				return &url.Error{Op: "validate", URL: target.String(), Err: errUnapprovedRedirect}
			}
			return nil
		},
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected redirect target to be rejected")
	}
}

func TestNewHTTPClientAuditsRejectedRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target?secret=redact", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var events []OutboundAuditEvent
	SetOutboundAuditHook(func(event OutboundAuditEvent) { events = append(events, event) })
	defer SetOutboundAuditHook(nil)
	client := NewHTTPClient(Config{
		AllowedHosts: []string{parsed.Hostname()},
		ValidateURL: func(target *url.URL) error {
			if target.Path == "/target" {
				return errUnapprovedRedirect
			}
			return nil
		},
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected redirect target to be rejected")
	}
	if len(events) != 2 || events[1].Allowed || !strings.Contains(events[1].Error, "unapproved redirect") {
		t.Fatalf("unexpected redirect audit events: %+v", events)
	}
	if strings.Contains(events[1].URL, "secret=redact") {
		t.Fatal("redirect audit event leaked query parameters")
	}
}

func TestNewHTTPClientValidatesResolvedIPBeforeDial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	client := NewHTTPClient(Config{
		AllowedHosts: []string{"127.0.0.1"},
		ValidateIP: func(ip net.IP) error {
			if ip.IsLoopback() {
				return fmt.Errorf("loopback is not approved")
			}
			return nil
		},
	})
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected resolved loopback IP to be rejected before dialing")
	}
}

func TestNewHTTPClientRevalidatesResolvedIPAfterRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var dialCount int32
	client := NewHTTPClient(Config{
		AllowedHosts: []string{parsed.Hostname()},
		ValidateIP: func(net.IP) error {
			if atomic.AddInt32(&dialCount, 1) > 1 {
				return errors.New("dns rebinding detected")
			}
			return nil
		},
	})
	request, err := http.NewRequest(http.MethodGet, server.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("expected resolved IP to be revalidated on redirect")
	}
	if atomic.LoadInt32(&dialCount) < 2 {
		t.Fatalf("expected at least two dial validations, got %d", dialCount)
	}
}

var errUnapprovedRedirect = errorString("unapproved redirect")

type errorString string

func (e errorString) Error() string { return string(e) }
