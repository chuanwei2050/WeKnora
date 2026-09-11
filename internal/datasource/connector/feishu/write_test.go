package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSanitizeErrorMessage_RedactsSecrets(t *testing.T) {
	in := `auth failed bearer u-abcdef0123456789 app_secret=s3cret-value tenant_access_token=t-xyz`
	out := SanitizeErrorMessage(in)
	if strings.Contains(out, "u-abcdef") || strings.Contains(out, "s3cret-value") || strings.Contains(out, "t-xyz") {
		t.Fatalf("secret leaked in sanitized message: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "app_secret") {
		t.Fatalf("expected field name retained, got %q", out)
	}
}

func TestMapAPIError_PermissionDenied(t *testing.T) {
	err := MapAPIError(http.StatusOK, CodePermissionDenied, "wiki space permission denied", "/wiki")
	var perm *PermissionDeniedError
	if !asPermissionDenied(err, &perm) {
		t.Fatalf("expected PermissionDeniedError, got %T %v", err, err)
	}
	if strings.Contains(err.Error(), "Bearer ") {
		t.Fatalf("error should not contain bearer token: %v", err)
	}
}

func TestMapAPIError_NotFound(t *testing.T) {
	err := MapAPIError(http.StatusNotFound, 0, "wiki node not found", "/wiki/v2/spaces/get_node")
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %T %v", err, err)
	}
	err = MapAPIError(http.StatusOK, 131004, "node does not exist", "/wiki")
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound for code 131004, got %T %v", err, err)
	}
	err = MapAPIError(http.StatusOK, CodePermissionDenied, "denied", "/wiki")
	if IsNotFound(err) {
		t.Fatalf("permission denied must not be not-found")
	}
}


func asPermissionDenied(err error, target **PermissionDeniedError) bool {
	for err != nil {
		if e, ok := err.(*PermissionDeniedError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestDoRequest_RetriesOn429(t *testing.T) {
	oldAttempts, oldDelay := maxRequestAttempts, retryBaseDelay
	maxRequestAttempts = 4
	retryBaseDelay = time.Millisecond
	t.Cleanup(func() {
		maxRequestAttempts = oldAttempts
		retryBaseDelay = oldDelay
	})

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "ok", "tenant_access_token": "t-test", "expire": 7200,
			})
			return
		}
		n := hits.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"code":99991400,"msg":"rate limit"}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"msg":  "ok",
			"data": map[string]any{
				"items":      []map[string]string{{"space_id": "s1", "name": "Space"}},
				"has_more":   false,
				"page_token": "",
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewOfficialClient("app", "secret")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()

	spaces, _, _, err := client.ListWikiSpacesPage(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("ListWikiSpacesPage: %v", err)
	}
	if hits.Load() < 3 {
		t.Fatalf("expected retries, hits=%d", hits.Load())
	}
	if len(spaces) != 1 || spaces[0].SpaceID != "s1" {
		t.Fatalf("unexpected spaces: %+v", spaces)
	}
}

func TestCreateWikiNode_RequestShape(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "tenant_access_token") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "t-test", "expire": 7200,
			})
			return
		}
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"node": map[string]any{
					"space_id": "sp", "node_token": "n1", "obj_token": "o1", "obj_type": "docx", "title": "Hello",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewOfficialClient("app", "secret")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()

	node, err := client.CreateWikiNode(context.Background(), "sp", "parent", "Hello", "docx")
	if err != nil {
		t.Fatalf("CreateWikiNode: %v", err)
	}
	if node.NodeToken != "n1" {
		t.Fatalf("node token=%s", node.NodeToken)
	}
	if gotBody["obj_type"] != "docx" || gotBody["node_type"] != "origin" || gotBody["title"] != "Hello" {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
	if gotBody["parent_node_token"] != "parent" {
		t.Fatalf("parent_node_token=%v", gotBody["parent_node_token"])
	}
}

func TestNewOfficialClient_ForcesDefaultBaseURL(t *testing.T) {
	c := NewOfficialClient("a", "b")
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL=%s want %s", c.baseURL, DefaultBaseURL)
	}
}

func TestResolveCreatedFileBlock_UnwrapsView(t *testing.T) {
	got, err := resolveCreatedFileBlock([]DocxBlock{{
		BlockID:   "view-1",
		BlockType: DocxBlockTypeView,
		Children:  []string{"file-1"},
	}})
	if err != nil {
		t.Fatalf("resolveCreatedFileBlock: %v", err)
	}
	if got.BlockID != "file-1" || got.ParentID != "view-1" || got.BlockType != DocxBlockTypeFile {
		t.Fatalf("unexpected block: %+v", got)
	}
}

func TestResolveCreatedFileBlock_DirectFile(t *testing.T) {
	got, err := resolveCreatedFileBlock([]DocxBlock{{
		BlockID:   "file-direct",
		BlockType: DocxBlockTypeFile,
		File:      &DocxFile{Token: ""},
	}})
	if err != nil {
		t.Fatalf("resolveCreatedFileBlock: %v", err)
	}
	if got.BlockID != "file-direct" {
		t.Fatalf("block_id=%s", got.BlockID)
	}
}

func TestResolveCreatedFileBlock_ViewWithoutChild(t *testing.T) {
	_, err := resolveCreatedFileBlock([]DocxBlock{{
		BlockID:   "view-empty",
		BlockType: DocxBlockTypeView,
	}})
	if err == nil {
		t.Fatal("expected error for view without file child")
	}
}

func TestUploadMediaStream_Multipart(t *testing.T) {
	const (
		fileSize  = uploadAllMaxBytes + 1024 // force multipart
		blockSize = 4 * 1024 * 1024
	)
	blockNum := (fileSize + blockSize - 1) / blockSize
	var parts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "t-test", "expire": 7200,
			})
		case strings.HasSuffix(r.URL.Path, "/medias/upload_prepare"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"upload_id":  "up-1",
					"block_size": blockSize,
					"block_num":  blockNum,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/medias/upload_part"):
			parts++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok"})
		case strings.HasSuffix(r.URL.Path, "/medias/upload_finish"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"file_token": "ft-1"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewOfficialClient("app", "secret")
	client.baseURL = srv.URL
	client.httpClient = srv.Client()

	payload := strings.Repeat("a", fileSize)
	token, err := client.UploadMediaStream(context.Background(), "block-1", "big.bin", int64(fileSize), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("UploadMediaStream: %v", err)
	}
	if token != "ft-1" {
		t.Fatalf("token=%s", token)
	}
	if parts != blockNum {
		t.Fatalf("parts=%d want=%d", parts, blockNum)
	}
}
