package structuredquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Request struct {
	Namespace  string   `json:"namespace"`
	Question   string   `json:"question"`
	DatasetIDs []string `json:"dataset_ids,omitempty"`
}

type Response struct {
	Route      string            `json:"route"`
	SQL        string            `json:"sql"`
	Columns    []string          `json:"columns"`
	Rows       [][]any           `json:"rows"`
	ModelCalls int               `json:"model_calls"`
	Timings    map[string]int64  `json:"timings"`
	Sources    []json.RawMessage `json:"sources"`
}

type Client struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
}

func (c Client) Query(ctx context.Context, tenantID uint64, request Request) (*Response, error) {
	if tenantID == 0 || strings.TrimSpace(request.Namespace) == "" || strings.TrimSpace(request.Question) == "" {
		return nil, fmt.Errorf("tenant, namespace and question are required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/query", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", c.APIKey)
		req.Header.Set("X-Tenant-ID", fmt.Sprint(tenantID))
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 && requestCtx.Err() == nil {
				continue
			}
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
			if attempt == 0 && requestCtx.Err() == nil && shouldRetryStructuredQueryStatus(resp.StatusCode, string(message)) {
				continue
			}
			return nil, lastErr
		}
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var result Response
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, err
		}
		if result.Route != "sql" && result.Route != "none" {
			return nil, fmt.Errorf("unexpected structured route %q", result.Route)
		}
		return &result, nil
	}
	return nil, lastErr
}

// DeleteDatasetsByPrefix removes sidecar datasets whose idempotency_key starts
// with prefix (typically "{knowledgeID}-"), including rebuild orphans.
func (c Client) DeleteDatasetsByPrefix(ctx context.Context, tenantID uint64, namespace, idempotencyPrefix string) (int, error) {
	namespace = strings.TrimSpace(namespace)
	idempotencyPrefix = strings.TrimSpace(idempotencyPrefix)
	if tenantID == 0 || namespace == "" || idempotencyPrefix == "" {
		return 0, fmt.Errorf("tenant, namespace and idempotency prefix are required")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	endpoint := fmt.Sprintf(
		"%s/v1/datasets?namespace=%s&idempotency_prefix=%s",
		strings.TrimRight(c.BaseURL, "/"),
		url.QueryEscape(namespace),
		url.QueryEscape(idempotencyPrefix),
	)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("X-Tenant-ID", fmt.Sprint(tenantID))
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var payload struct {
		Deleted int `json:"deleted"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Deleted, nil
}

func shouldRetryStructuredQueryStatus(status int, body string) bool {
	if status == http.StatusTooManyRequests || status >= 500 {
		return true
	}
	if status != http.StatusUnprocessableEntity {
		return false
	}
	return strings.Contains(body, "model_unavailable") || strings.Contains(body, "model_timeout")
}
