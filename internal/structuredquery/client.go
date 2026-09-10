package structuredquery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("X-Tenant-ID", fmt.Sprint(tenantID))
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var result Response
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if result.Route != "sql" && result.Route != "none" {
		return nil, fmt.Errorf("unexpected structured route %q", result.Route)
	}
	return &result, nil
}
