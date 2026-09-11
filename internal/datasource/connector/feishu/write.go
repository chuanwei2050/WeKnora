package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

// mediaUploadTimeout bounds a single media upload HTTP call (prepare/part/finish/all).
const mediaUploadTimeout = 5 * time.Minute

// typesFeishuPublishMaxUpload mirrors the product soft ceiling without importing types (cycle risk).
func typesFeishuPublishMaxUpload() int64 {
	return 2 * 1024 * 1024 * 1024
}

// ListWikiSpacesPage returns one page of wiki spaces.
func (c *Client) ListWikiSpacesPage(ctx context.Context, pageToken string, pageSize int) (spaces []WikiSpace, hasMore bool, nextToken string, err error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	path := "/open-apis/wiki/v2/spaces?" + q.Encode()

	var resp wikiSpaceListResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, false, "", fmt.Errorf("list wiki spaces: %w", err)
	}
	if resp.Code != 0 {
		logger.Errorf(ctx, "[Feishu] ListWikiSpacesPage error: code=%d msg=%s", resp.Code, SanitizeErrorMessage(resp.Msg))
		return nil, false, "", MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}

	logger.Infof(ctx, "[Feishu] ListWikiSpacesPage: got %d spaces, has_more=%v", len(resp.Data.Items), resp.Data.HasMore)
	for i, s := range resp.Data.Items {
		logger.Infof(ctx, "[Feishu]   space[%d]: id=%s name=%q visibility=%s", i, s.SpaceID, s.Name, s.Visibility)
	}

	return resp.Data.Items, resp.Data.HasMore, resp.Data.PageToken, nil
}

// ListWikiNodesPage returns one page of nodes under a wiki space / parent.
func (c *Client) ListWikiNodesPage(ctx context.Context, spaceID, parentNodeToken, pageToken string, pageSize int) (nodes []WikiNode, hasMore bool, nextToken string, err error) {
	if spaceID == "" {
		return nil, false, "", fmt.Errorf("list wiki nodes: spaceID is required")
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	if parentNodeToken != "" {
		q.Set("parent_node_token", parentNodeToken)
	}
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	path := fmt.Sprintf("/open-apis/wiki/v2/spaces/%s/nodes?%s", url.PathEscape(spaceID), q.Encode())

	var resp wikiNodeListResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, false, "", fmt.Errorf("list wiki nodes: %w", err)
	}
	if resp.Code != 0 {
		return nil, false, "", MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}

	return resp.Data.Items, resp.Data.HasMore, resp.Data.PageToken, nil
}

// GetWikiNode fetches a single wiki node by its token.
// GET /open-apis/wiki/v2/spaces/get_node?token=
func (c *Client) GetWikiNode(ctx context.Context, token string) (*WikiNode, error) {
	if token == "" {
		return nil, fmt.Errorf("get wiki node: token is required")
	}
	q := url.Values{}
	q.Set("token", token)
	path := "/open-apis/wiki/v2/spaces/get_node?" + q.Encode()

	var resp wikiNodeInfoResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("get wiki node: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	node := resp.Data.Node
	return &node, nil
}

// CreateWikiNode creates a wiki node under spaceID / parentNodeToken.
// objType defaults to "docx". node_type is always "origin".
func (c *Client) CreateWikiNode(ctx context.Context, spaceID, parentNodeToken, title, objType string) (*WikiNode, error) {
	if spaceID == "" {
		return nil, fmt.Errorf("create wiki node: spaceID is required")
	}
	if objType == "" {
		objType = "docx"
	}
	body := CreateWikiNodeRequest{
		ObjType:         objType,
		ParentNodeToken: parentNodeToken,
		NodeType:        "origin",
		Title:           title,
	}
	path := fmt.Sprintf("/open-apis/wiki/v2/spaces/%s/nodes", url.PathEscape(spaceID))

	var resp wikiNodeMutationResponse
	if err := c.doRequest(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, fmt.Errorf("create wiki node: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	node := resp.Data.Node
	return &node, nil
}

// UpdateWikiNodeTitle updates the title of an existing wiki node.
// Feishu API: POST /wiki/v2/spaces/:space_id/nodes/:node_token/update_title
func (c *Client) UpdateWikiNodeTitle(ctx context.Context, spaceID, nodeToken, title string) (*WikiNode, error) {
	if spaceID == "" || nodeToken == "" {
		return nil, fmt.Errorf("update wiki node title: spaceID and nodeToken are required")
	}
	body := UpdateWikiNodeTitleRequest{Title: title}
	path := fmt.Sprintf("/open-apis/wiki/v2/spaces/%s/nodes/%s/update_title", url.PathEscape(spaceID), url.PathEscape(nodeToken))

	var resp wikiNodeMutationResponse
	if err := c.doRequest(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, fmt.Errorf("update wiki node title: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	node := resp.Data.Node
	if node.NodeToken == "" {
		node.NodeToken = nodeToken
		node.Title = title
	}
	return &node, nil
}

// MoveWikiNode moves a node under a new parent within the same space.
func (c *Client) MoveWikiNode(ctx context.Context, spaceID, nodeToken, targetParentToken string) (*WikiNode, error) {
	if spaceID == "" || nodeToken == "" {
		return nil, fmt.Errorf("move wiki node: spaceID and nodeToken are required")
	}
	body := MoveWikiNodeRequest{
		TargetParentToken: targetParentToken,
		TargetSpaceID:     spaceID,
	}
	path := fmt.Sprintf("/open-apis/wiki/v2/spaces/%s/nodes/%s/move", url.PathEscape(spaceID), url.PathEscape(nodeToken))

	var resp wikiNodeMutationResponse
	if err := c.doRequest(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, fmt.Errorf("move wiki node: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	node := resp.Data.Node
	return &node, nil
}

// CreateBlockChildren inserts child blocks under parentBlockID.
// For document-level children, pass documentID as parentBlockID.
func (c *Client) CreateBlockChildren(ctx context.Context, documentID, parentBlockID string, children []DocxBlock, index int) ([]DocxBlock, error) {
	if documentID == "" || parentBlockID == "" {
		return nil, fmt.Errorf("create block children: documentID and parentBlockID are required")
	}
	body := CreateBlockChildrenRequest{Children: children, Index: index}
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/blocks/%s/children",
		url.PathEscape(documentID), url.PathEscape(parentBlockID))

	var resp CreateBlockChildrenResponse
	if err := c.doRequest(ctx, http.MethodPost, path, body, &resp); err != nil {
		return nil, fmt.Errorf("create block children: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	return resp.Data.Children, nil
}

// ListDocumentBlocksPage lists blocks of a document (one page).
func (c *Client) ListDocumentBlocksPage(ctx context.Context, documentID, pageToken string, pageSize int) (blocks []DocxBlock, hasMore bool, nextToken string, err error) {
	if documentID == "" {
		return nil, false, "", fmt.Errorf("list document blocks: documentID is required")
	}
	if pageSize <= 0 {
		pageSize = 500
	}
	q := url.Values{}
	q.Set("page_size", strconv.Itoa(pageSize))
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/blocks?%s", url.PathEscape(documentID), q.Encode())

	var resp ListDocumentBlocksResponse
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, false, "", fmt.Errorf("list document blocks: %w", err)
	}
	if resp.Code != 0 {
		return nil, false, "", MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	return resp.Data.Items, resp.Data.HasMore, resp.Data.PageToken, nil
}

// ListDocumentBlocks returns all blocks of a document (paginated under the hood).
func (c *Client) ListDocumentBlocks(ctx context.Context, documentID string) ([]DocxBlock, error) {
	var all []DocxBlock
	pageToken := ""
	for {
		blocks, hasMore, next, err := c.ListDocumentBlocksPage(ctx, documentID, pageToken, 500)
		if err != nil {
			return nil, err
		}
		all = append(all, blocks...)
		if !hasMore || next == "" {
			break
		}
		pageToken = next
	}
	return all, nil
}

// BatchDeleteBlockChildren deletes children in [startIndex, endIndex) under parentBlockID.
func (c *Client) BatchDeleteBlockChildren(ctx context.Context, documentID, parentBlockID string, startIndex, endIndex int) error {
	if documentID == "" || parentBlockID == "" {
		return fmt.Errorf("batch delete block children: documentID and parentBlockID are required")
	}
	body := BatchDeleteBlockChildrenRequest{StartIndex: startIndex, EndIndex: endIndex}
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/blocks/%s/children/batch_delete",
		url.PathEscape(documentID), url.PathEscape(parentBlockID))

	var resp apiResponse
	if err := c.doRequest(ctx, http.MethodDelete, path, body, &resp); err != nil {
		return fmt.Errorf("batch delete block children: %w", err)
	}
	if resp.Code != 0 {
		return MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	return nil
}

// ReplaceFileInBlock sets a new media token on an existing file block.
func (c *Client) ReplaceFileInBlock(ctx context.Context, documentID, blockID, fileToken string) (*DocxBlock, error) {
	if documentID == "" || blockID == "" || fileToken == "" {
		return nil, fmt.Errorf("replace file in block: documentID, blockID and fileToken are required")
	}
	body := ReplaceFileRequest{ReplaceFile: &ReplaceFilePayload{Token: fileToken}}
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/blocks/%s",
		url.PathEscape(documentID), url.PathEscape(blockID))

	var resp UpdateBlockResponse
	if err := c.doRequest(ctx, http.MethodPatch, path, body, &resp); err != nil {
		return nil, fmt.Errorf("replace file in block: %w", err)
	}
	if resp.Code != 0 {
		return nil, MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	block := resp.Data.Block
	return &block, nil
}

// CreateFileBlock creates an empty file block under parentBlockID and returns the File Block
// (not the wrapping View Block that Feishu returns as the create-children top-level item).
func (c *Client) CreateFileBlock(ctx context.Context, documentID, parentBlockID string, index int) (*DocxBlock, error) {
	created, err := c.CreateBlockChildren(ctx, documentID, parentBlockID, []DocxBlock{NewEmptyFileBlock()}, index)
	if err != nil {
		return nil, err
	}
	return resolveCreatedFileBlock(created)
}

// resolveCreatedFileBlock extracts the File Block from Feishu's create-children response.
// Feishu wraps each File Block in a View Block (type 33); upload/replace_file must target
// the File Block ID in view.children[0], otherwise PATCH returns 1770025.
func resolveCreatedFileBlock(created []DocxBlock) (*DocxBlock, error) {
	if len(created) == 0 {
		return nil, fmt.Errorf("create file block: empty response")
	}
	for i := range created {
		b := &created[i]
		if b.BlockType == DocxBlockTypeFile {
			return b, nil
		}
		if b.BlockType == DocxBlockTypeView {
			if len(b.Children) == 0 || b.Children[0] == "" {
				return nil, fmt.Errorf("create file block: view block %q has no file child", b.BlockID)
			}
			return &DocxBlock{
				BlockID:   b.Children[0],
				ParentID:  b.BlockID,
				BlockType: DocxBlockTypeFile,
				File:      &DocxFile{Token: ""},
			}, nil
		}
	}
	return nil, fmt.Errorf("create file block: unexpected block_type=%d (want file=%d or view=%d)",
		created[0].BlockType, DocxBlockTypeFile, DocxBlockTypeView)
}

// UploadMedia uploads file bytes as drive media for a docx file block.
// Uses upload_all for files <= 20MiB; otherwise prepare/part/finish.
func (c *Client) UploadMedia(ctx context.Context, parentNode, fileName string, data []byte) (fileToken string, err error) {
	return c.UploadMediaStream(ctx, parentNode, fileName, int64(len(data)), bytes.NewReader(data))
}

// UploadMediaStream uploads from a reader without requiring the full file in memory.
// size must match the exact number of bytes that will be read from r.
func (c *Client) UploadMediaStream(ctx context.Context, parentNode, fileName string, size int64, r io.Reader) (fileToken string, err error) {
	if parentNode == "" || fileName == "" {
		return "", fmt.Errorf("upload media: parentNode and fileName are required")
	}
	if r == nil {
		return "", fmt.Errorf("upload media: reader is required")
	}
	if size <= 0 {
		return "", fmt.Errorf("upload media: empty file")
	}
	if size > typesFeishuPublishMaxUpload() {
		return "", fmt.Errorf("upload media: file exceeds %d bytes", typesFeishuPublishMaxUpload())
	}

	if size <= uploadAllMaxBytes {
		data, err := io.ReadAll(io.LimitReader(r, size+1))
		if err != nil {
			return "", fmt.Errorf("upload media: read file: %w", err)
		}
		if int64(len(data)) != size {
			return "", fmt.Errorf("upload media: declared size %d != actual %d", size, len(data))
		}
		return c.uploadMediaAll(ctx, parentNode, fileName, data)
	}
	return c.uploadMediaMultipartStream(ctx, parentNode, fileName, size, r)
}

func (c *Client) uploadMediaAll(ctx context.Context, parentNode, fileName string, data []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	_ = w.WriteField("file_name", fileName)
	_ = w.WriteField("parent_type", UploadMediaParentTypeDocxFile)
	_ = w.WriteField("parent_node", parentNode)
	_ = w.WriteField("size", strconv.Itoa(len(data)))

	part, err := w.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("upload_all create part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("upload_all write file: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("upload_all close writer: %w", err)
	}

	path := "/open-apis/drive/v1/medias/upload_all"
	var resp UploadMediaAllResponse
	if err := c.doRequestWithOptions(ctx, http.MethodPost, path, nil, &resp, requestOptions{
		ContentType: w.FormDataContentType(),
		RawBody:     buf.Bytes(),
		Timeout:     mediaUploadTimeout,
	}); err != nil {
		return "", fmt.Errorf("upload_all: %w", err)
	}
	if resp.Code != 0 {
		return "", MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	if resp.Data.FileToken == "" {
		return "", fmt.Errorf("upload_all: empty file_token")
	}
	return resp.Data.FileToken, nil
}

func (c *Client) uploadMediaMultipartStream(ctx context.Context, parentNode, fileName string, size int64, r io.Reader) (string, error) {
	preparePath := "/open-apis/drive/v1/medias/upload_prepare"
	var prepareResp UploadMediaPrepareResponse
	if err := c.doRequestWithOptions(ctx, http.MethodPost, preparePath, UploadMediaPrepareRequest{
		FileName:   fileName,
		ParentType: UploadMediaParentTypeDocxFile,
		ParentNode: parentNode,
		Size:       size,
	}, &prepareResp, requestOptions{Timeout: mediaUploadTimeout}); err != nil {
		return "", fmt.Errorf("upload_prepare: %w", err)
	}
	if prepareResp.Code != 0 {
		return "", MapAPIError(http.StatusOK, prepareResp.Code, prepareResp.Msg, preparePath)
	}
	uploadID := prepareResp.Data.UploadID
	blockSize := prepareResp.Data.BlockSize
	blockNum := prepareResp.Data.BlockNum
	if uploadID == "" || blockSize <= 0 || blockNum <= 0 {
		return "", fmt.Errorf("upload_prepare: invalid plan upload_id=%q block_size=%d block_num=%d", uploadID, blockSize, blockNum)
	}

	buf := make([]byte, blockSize)
	var readTotal int64
	for seq := 0; seq < blockNum; seq++ {
		remaining := size - readTotal
		if remaining <= 0 {
			return "", fmt.Errorf("upload_part: no bytes left before seq=%d/%d", seq, blockNum)
		}
		toRead := int64(blockSize)
		if toRead > remaining {
			toRead = remaining
		}
		chunk := buf[:toRead]
		if _, err := io.ReadFull(r, chunk); err != nil {
			return "", fmt.Errorf("upload_part read seq=%d: %w", seq, err)
		}
		if err := c.uploadMediaPart(ctx, uploadID, seq, chunk); err != nil {
			return "", err
		}
		readTotal += toRead
	}
	if readTotal != size {
		return "", fmt.Errorf("upload media: read %d bytes, expected %d", readTotal, size)
	}

	finishPath := "/open-apis/drive/v1/medias/upload_finish"
	var finishResp UploadMediaFinishResponse
	if err := c.doRequestWithOptions(ctx, http.MethodPost, finishPath, UploadMediaFinishRequest{
		UploadID: uploadID,
		BlockNum: blockNum,
	}, &finishResp, requestOptions{Timeout: mediaUploadTimeout}); err != nil {
		return "", fmt.Errorf("upload_finish: %w", err)
	}
	if finishResp.Code != 0 {
		return "", MapAPIError(http.StatusOK, finishResp.Code, finishResp.Msg, finishPath)
	}
	if finishResp.Data.FileToken == "" {
		return "", fmt.Errorf("upload_finish: empty file_token")
	}
	return finishResp.Data.FileToken, nil
}

func (c *Client) uploadMediaPart(ctx context.Context, uploadID string, seq int, chunk []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("upload_id", uploadID)
	_ = w.WriteField("seq", strconv.Itoa(seq))
	_ = w.WriteField("size", strconv.Itoa(len(chunk)))

	part, err := w.CreateFormFile("file", "blob")
	if err != nil {
		return fmt.Errorf("upload_part create form: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(chunk)); err != nil {
		return fmt.Errorf("upload_part write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("upload_part close: %w", err)
	}

	path := "/open-apis/drive/v1/medias/upload_part"
	var resp apiResponse
	if err := c.doRequestWithOptions(ctx, http.MethodPost, path, nil, &resp, requestOptions{
		ContentType: w.FormDataContentType(),
		RawBody:     buf.Bytes(),
		Timeout:     mediaUploadTimeout,
	}); err != nil {
		return fmt.Errorf("upload_part seq=%d: %w", seq, err)
	}
	if resp.Code != 0 {
		return MapAPIError(http.StatusOK, resp.Code, resp.Msg, path)
	}
	return nil
}

// CreateFileBlockAndUpload creates an empty file block, uploads media, and binds the token.
func (c *Client) CreateFileBlockAndUpload(ctx context.Context, documentID, parentBlockID, fileName string, data []byte, index int) (block *DocxBlock, fileToken string, err error) {
	return c.CreateFileBlockAndUploadStream(ctx, documentID, parentBlockID, fileName, int64(len(data)), bytes.NewReader(data), index)
}

// CreateFileBlockAndUploadStream creates an empty file block and streams media upload into it.
func (c *Client) CreateFileBlockAndUploadStream(ctx context.Context, documentID, parentBlockID, fileName string, size int64, r io.Reader, index int) (block *DocxBlock, fileToken string, err error) {
	block, err = c.CreateFileBlock(ctx, documentID, parentBlockID, index)
	if err != nil {
		return nil, "", err
	}
	fileToken, err = c.UploadMediaStream(ctx, block.BlockID, fileName, size, r)
	if err != nil {
		return block, "", err
	}
	updated, err := c.ReplaceFileInBlock(ctx, documentID, block.BlockID, fileToken)
	if err != nil {
		return block, fileToken, err
	}
	return updated, fileToken, nil
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
