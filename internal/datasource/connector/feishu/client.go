package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Client wraps the Feishu Open Platform API for document/wiki operations.
type Client struct {
	baseURL   string
	appID     string
	appSecret string

	httpClient *http.Client

	// Token cache (thread-safe)
	tokenMu    sync.Mutex
	tokenCache string
	tokenExpAt time.Time
}

// retry knobs (overridable in tests).
var (
	maxRequestAttempts = 6 // 1 initial + up to 5 retries
	retryBaseDelay     = 200 * time.Millisecond
)

// NewClient creates a new Feishu API client.
func NewClient(config *Config) *Client {
	return &Client{
		baseURL:    config.GetBaseURL(),
		appID:      config.AppID,
		appSecret:  config.AppSecret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewOfficialClient builds a Client that always uses the official DefaultBaseURL.
// Publish paths must call this so callers cannot inject a custom endpoint.
func NewOfficialClient(appID, appSecret string) *Client {
	return NewClient(&Config{
		AppID:     appID,
		AppSecret: appSecret,
		BaseURL:   DefaultBaseURL,
	})
}

// NewClientWithEndpoint uses the administrator-approved transport when the
// data source is bound to a private endpoint.
func NewClientWithEndpoint(config *Config, endpoint *types.ApprovedEndpoint) (*Client, error) {
	client := NewClient(config)
	if endpoint == nil {
		return client, nil
	}
	httpClient, err := datasource.NewApprovedEndpointHTTPClient(endpoint, 30*time.Second)
	if err != nil {
		return nil, err
	}
	client.httpClient = httpClient
	return client, nil
}

// getTenantAccessToken retrieves (or returns cached) tenant access token.
// Feishu tokens expire in 2 hours; we cache with a 5-minute safety margin.
func (c *Client) getTenantAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.tokenCache != "" && time.Now().Before(c.tokenExpAt) {
		return c.tokenCache, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})

	url := c.baseURL + "/open-apis/auth/v3/tenant_access_token/internal"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()

	var result tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBodyBytes+1)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if result.Code != 0 {
		return "", MapAPIError(resp.StatusCode, result.Code, result.Msg, "/open-apis/auth/v3/tenant_access_token/internal")
	}

	c.tokenCache = result.TenantAccessToken
	ttl := time.Duration(result.Expire) * time.Second
	if ttl > 5*time.Minute {
		ttl -= 5 * time.Minute
	}
	c.tokenExpAt = time.Now().Add(ttl)

	prefixLen := 8
	if len(result.TenantAccessToken) < prefixLen {
		prefixLen = len(result.TenantAccessToken)
	}
	suffixLen := 4
	if len(result.TenantAccessToken) < suffixLen {
		suffixLen = len(result.TenantAccessToken)
	}
	logger.Infof(ctx, "[Feishu] got tenant_access_token: %s...%s expire=%ds",
		result.TenantAccessToken[:prefixLen], result.TenantAccessToken[len(result.TenantAccessToken)-suffixLen:], result.Expire)

	return c.tokenCache, nil
}

// requestOptions customizes a single HTTP call.
type requestOptions struct {
	// ContentType overrides the default JSON content type when set.
	ContentType string
	// RawBody sends opaque bytes instead of JSON-marshaled Body.
	RawBody []byte
	// SkipRetry disables 429/lock retries.
	SkipRetry bool
	// Timeout overrides the client timeout for this request when > 0.
	Timeout time.Duration
}

func (c *Client) httpClientFor(opts requestOptions) *http.Client {
	if opts.Timeout <= 0 || c.httpClient == nil {
		return c.httpClient
	}
	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: c.httpClient.Transport,
	}
}

// doRequest executes an authenticated JSON API request and decodes the response.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	return c.doRequestWithOptions(ctx, method, path, body, result, requestOptions{})
}

// doRequestWithOptions is the shared authenticated request path with size limits,
// typed rate-limit errors, and finite exponential backoff.
func (c *Client) doRequestWithOptions(ctx context.Context, method, path string, body interface{}, result interface{}, opts requestOptions) error {
	var bodyBytes []byte
	var err error
	if opts.RawBody != nil {
		bodyBytes = opts.RawBody
	} else if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
	}

	attempts := maxRequestAttempts
	if opts.SkipRetry {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			delay := retryDelay(attempt - 1)
			logger.Infof(ctx, "[Feishu] retry %d/%d after %s for %s %s", attempt-1, attempts-1, delay, method, path)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = c.doRequestOnce(ctx, method, path, bodyBytes, result, opts)
		if lastErr == nil {
			return nil
		}
		if !IsRetryable(lastErr) || attempt == attempts {
			if re, ok := lastErr.(*RetryableError); ok {
				re.Attempts = attempt
			}
			return lastErr
		}
	}
	return lastErr
}

func retryDelay(retryNum int) time.Duration {
	// Exponential: base * 2^(retryNum-1), capped.
	d := retryBaseDelay
	for i := 1; i < retryNum; i++ {
		d *= 2
	}
	const maxDelay = 5 * time.Second
	if d > maxDelay {
		return maxDelay
	}
	return d
}

func (c *Client) doRequestOnce(ctx context.Context, method, path string, bodyBytes []byte, result interface{}, opts requestOptions) error {
	token, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	ct := opts.ContentType
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer "+token)

	logger.Infof(ctx, "[Feishu] %s %s", method, path)

	resp, err := c.httpClientFor(opts).Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBodyBytes+1)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if len(respBody) > maxResponseBodyBytes {
		return fmt.Errorf("feishu api error: response body exceeds %d bytes", maxResponseBodyBytes)
	}

	safeBody := SanitizeErrorMessage(truncate(string(respBody), 1000))
	logger.Infof(ctx, "[Feishu] %s %s → status=%d bodyLen=%d body=%s",
		method, path, resp.StatusCode, len(respBody), safeBody)

	// Peek business code for retry / typed mapping even on non-200.
	var probe apiResponse
	_ = json.Unmarshal(respBody, &probe)

	if resp.StatusCode == http.StatusTooManyRequests || isRetryableCode(resp.StatusCode, probe.Code) {
		return &RetryableError{
			APIError: &APIError{
				HTTPStatus: resp.StatusCode,
				Code:       probe.Code,
				Msg:        SanitizeErrorMessage(probe.Msg),
				Path:       path,
			},
		}
	}

	if resp.StatusCode != http.StatusOK {
		msg := probe.Msg
		if msg == "" {
			msg = truncate(string(respBody), 200)
		}
		return MapAPIError(resp.StatusCode, probe.Code, msg, path)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	// HTTP 200 with retryable business code (rate limit / lock) — treat as retryable.
	if isRetryableCode(http.StatusOK, probe.Code) {
		return &RetryableError{
			APIError: &APIError{
				HTTPStatus: resp.StatusCode,
				Code:       probe.Code,
				Msg:        SanitizeErrorMessage(probe.Msg),
				Path:       path,
			},
		}
	}

	return nil
}

// truncate truncates a string to maxLen and appends "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ListWikiSpaces returns all wiki spaces accessible to the app.
func (c *Client) ListWikiSpaces(ctx context.Context) ([]wikiSpace, error) {
	var allSpaces []wikiSpace
	pageToken := ""

	for {
		spaces, hasMore, next, err := c.ListWikiSpacesPage(ctx, pageToken, 50)
		if err != nil {
			return nil, err
		}
		allSpaces = append(allSpaces, spaces...)
		if !hasMore || next == "" {
			break
		}
		pageToken = next
	}

	logger.Infof(ctx, "[Feishu] ListWikiSpaces: total %d spaces", len(allSpaces))
	return allSpaces, nil
}

// ListWikiNodes returns all nodes (documents) under a wiki space.
// If parentNodeToken is empty, returns top-level nodes.
func (c *Client) ListWikiNodes(ctx context.Context, spaceID string, parentNodeToken string) ([]wikiNode, error) {
	var allNodes []wikiNode
	pageToken := ""

	for {
		nodes, hasMore, next, err := c.ListWikiNodesPage(ctx, spaceID, parentNodeToken, pageToken, 50)
		if err != nil {
			return nil, err
		}
		allNodes = append(allNodes, nodes...)
		if !hasMore || next == "" {
			break
		}
		pageToken = next
	}

	return allNodes, nil
}

// ListAllWikiNodesRecursive recursively lists all nodes under a wiki space.
// It walks the tree depth-first to discover all nested documents.
func (c *Client) ListAllWikiNodesRecursive(ctx context.Context, spaceID string) ([]wikiNode, error) {
	// Start with top-level nodes
	topNodes, err := c.ListWikiNodes(ctx, spaceID, "")
	if err != nil {
		return nil, err
	}

	var allNodes []wikiNode
	var walk func(nodes []wikiNode) error

	walk = func(nodes []wikiNode) error {
		for _, node := range nodes {
			allNodes = append(allNodes, node)

			// Recurse into child nodes if this node has children
			if node.HasChild {
				children, err := c.ListWikiNodes(ctx, spaceID, node.NodeToken)
				if err != nil {
					return fmt.Errorf("list children of %s: %w", node.NodeToken, err)
				}
				if err := walk(children); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(topNodes); err != nil {
		return nil, err
	}

	return allNodes, nil
}

// GetDocumentRawContent retrieves the raw text content of a Feishu docx document.
// This returns plain text (not rich text / block structure).
// Deprecated: prefer ExportAndDownload which preserves formatting.
func (c *Client) GetDocumentRawContent(ctx context.Context, documentID string) (string, error) {
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/raw_content", documentID)

	var resp docRawContentResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", fmt.Errorf("get document raw content: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("get document raw content error: code=%d msg=%s", resp.Code, SanitizeErrorMessage(resp.Msg))
	}

	return resp.Data.Content, nil
}

// Ping verifies the credentials by attempting to get a tenant access token.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.getTenantAccessToken(ctx)
	return err
}

// ──────────────────────────────────────────────────────────────────────
// Export task API: export docx/sheet/bitable to downloadable files
//
// Flow:
//  1. POST  /drive/v1/export_tasks             → create export task, get ticket
//  2. GET   /drive/v1/export_tasks/:ticket      → poll until status=0 (success)
//  3. GET   /drive/v1/export_tasks/file/:ticket/download → download file bytes
// ──────────────────────────────────────────────────────────────────────

// CreateExportTask creates an async export task for a Feishu document.
//   - token:         the obj_token of the document (e.g. docx token, sheet token)
//   - objType:       the Feishu obj_type ("docx", "doc", "sheet", "bitable")
//   - fileExtension: desired output format ("docx", "xlsx", "pdf")
func (c *Client) CreateExportTask(ctx context.Context, token, objType, fileExtension string) (string, error) {
	body := map[string]string{
		"file_extension": fileExtension,
		"token":          token,
		"type":           objType,
	}

	var resp exportTaskCreateResponse
	if err := c.doRequest(ctx, http.MethodPost, "/open-apis/drive/v1/export_tasks", body, &resp); err != nil {
		return "", fmt.Errorf("create export task: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("create export task error: code=%d msg=%s", resp.Code, SanitizeErrorMessage(resp.Msg))
	}

	return resp.Data.Ticket, nil
}

// GetExportTaskStatus polls the status of an export task.
// Returns (fileToken, fileName, error). fileToken is non-empty only when the job succeeds.
// The token parameter is the obj_token of the document being exported (required by the API).
func (c *Client) GetExportTaskStatus(ctx context.Context, ticket string, token string) (string, string, error) {
	path := fmt.Sprintf("/open-apis/drive/v1/export_tasks/%s?token=%s", ticket, token)

	var resp exportTaskStatusResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return "", "", fmt.Errorf("get export task status: %w", err)
	}
	if resp.Code != 0 {
		return "", "", fmt.Errorf("get export task status error: code=%d msg=%s", resp.Code, SanitizeErrorMessage(resp.Msg))
	}

	r := resp.Data.Result
	switch r.JobStatus {
	case 0: // success
		return r.FileToken, r.FileName, nil
	case 1, 2: // initializing, processing
		return "", "", nil // not ready yet
	default:
		return "", "", fmt.Errorf("export task failed: status=%d msg=%s", r.JobStatus, SanitizeErrorMessage(r.JobErrorMsg))
	}
}

// DownloadExportFile downloads the exported file by its file_token.
// The file_token is returned by GetExportTaskStatus when the export job completes.
// The file must be downloaded within 10 minutes of export completion.
func (c *Client) DownloadExportFile(ctx context.Context, fileToken string) ([]byte, error) {
	path := fmt.Sprintf("/open-apis/drive/v1/export_tasks/file/%s/download", fileToken)
	return c.downloadRawBytes(ctx, path)
}

// ExportAndDownload is a high-level helper that creates an export task, polls until
// completion, and downloads the resulting file. Returns (fileBytes, fileName, error).
//
// Timeout: 60 seconds. Poll interval: 2 seconds.
func (c *Client) ExportAndDownload(ctx context.Context, objToken, objType string) ([]byte, string, error) {
	// Determine export format
	fileExt, ok := objTypeToExportFileExtension[objType]
	if !ok {
		return nil, "", fmt.Errorf("unsupported obj_type for export: %s", objType)
	}

	exportType, ok := objTypeToExportType[objType]
	if !ok {
		return nil, "", fmt.Errorf("unsupported obj_type for export: %s", objType)
	}

	// Step 1: create export task
	ticket, err := c.CreateExportTask(ctx, objToken, exportType, fileExt)
	if err != nil {
		return nil, "", err
	}

	// Step 2: poll until ready (max 60s, every 2s)
	deadline := time.Now().Add(60 * time.Second)
	var fileToken, fileName string

	for time.Now().Before(deadline) {
		fileToken, fileName, err = c.GetExportTaskStatus(ctx, ticket, objToken)
		if err != nil {
			return nil, "", err
		}
		if fileToken != "" {
			break // export ready
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if fileToken == "" {
		return nil, "", fmt.Errorf("export task timed out after 60s (ticket=%s)", ticket)
	}

	// Step 3: download file using file_token (NOT ticket)
	data, err := c.DownloadExportFile(ctx, fileToken)
	if err != nil {
		return nil, "", err
	}

	// Build a sensible file name
	if fileName == "" {
		fileName = "export" + exportFileExtToSuffix[fileExt]
	}

	return data, fileName, nil
}

// ──────────────────────────────────────────────────────────────────────
// Drive file download: for "file" type wiki nodes (uploaded PDF/Word/etc.)
// ──────────────────────────────────────────────────────────────────────

// DownloadDriveFile downloads a file from Feishu Drive by its file token.
// Used for wiki nodes with obj_type="file" (user-uploaded PDF, Word, images, etc.).
func (c *Client) DownloadDriveFile(ctx context.Context, fileToken string) ([]byte, error) {
	path := fmt.Sprintf("/open-apis/drive/v1/files/%s/download", fileToken)
	return c.downloadRawBytes(ctx, path)
}

// maxDownloadBodyBytes allows binary downloads up to the product soft ceiling.
const maxDownloadBodyBytes = 100 * 1024 * 1024

// downloadRawBytes performs an authenticated GET and returns the raw response body.
func (c *Client) downloadRawBytes(ctx context.Context, path string) ([]byte, error) {
	token, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	logger.Infof(ctx, "[Feishu] download GET %s", path)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
		logger.Errorf(ctx, "[Feishu] download GET %s → status=%d body=%s", path, resp.StatusCode, SanitizeErrorMessage(truncate(string(body), 500)))
		return nil, MapAPIError(resp.StatusCode, 0, truncate(string(body), 200), path)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read download body: %w", err)
	}
	if len(data) > maxDownloadBodyBytes {
		return nil, fmt.Errorf("download failed: body exceeds %d bytes", maxDownloadBodyBytes)
	}

	logger.Infof(ctx, "[Feishu] download GET %s → OK, %d bytes", path, len(data))
	return data, nil
}
