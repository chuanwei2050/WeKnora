package feishu

// Exported DTOs for Feishu Wiki / Docx write & paginated read APIs.
// Unexported aliases in types.go keep the pull connector compiling unchanged.

// WikiSpace represents a Feishu Wiki space (exported for publish callers).
type WikiSpace struct {
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"` // "public" or "private"
}

// WikiNode represents a node (document or folder) in a Feishu Wiki space.
type WikiNode struct {
	SpaceID        string `json:"space_id"`
	NodeToken      string `json:"node_token"`
	ObjToken       string `json:"obj_token"`
	ObjType        string `json:"obj_type"`
	ParentNodeID   string `json:"parent_node_id"`
	ParentNodeToken string `json:"parent_node_token,omitempty"`
	NodeType       string `json:"node_type"`
	OriginNodeID   string `json:"origin_node_id"`
	OriginSpaceID  string `json:"origin_space_id"`
	HasChild       bool   `json:"has_child"`
	Title          string `json:"title"`
	Creator        string `json:"creator"`
	Owner          string `json:"owner"`
	ObjCreateTime  string `json:"obj_create_time"`
	ObjEditTime    string `json:"obj_edit_time"`
	NodeCreateTime string `json:"node_create_time"`
	NodeEditTime   string `json:"node_edit_time"`
}

// CreateWikiNodeRequest is the body for POST /wiki/v2/spaces/:space_id/nodes.
type CreateWikiNodeRequest struct {
	ObjType         string `json:"obj_type"`
	ParentNodeToken string `json:"parent_node_token,omitempty"`
	NodeType        string `json:"node_type"`
	Title           string `json:"title,omitempty"`
	OriginNodeToken string `json:"origin_node_token,omitempty"`
}

// CreateWikiNodeResponse wraps the created node.
type CreateWikiNodeResponse struct {
	apiResponse
	Data struct {
		Node WikiNode `json:"node"`
	} `json:"data"`
}

// UpdateWikiNodeTitleRequest is the body for POST /wiki/v2/spaces/:space_id/nodes/:node_token/update_title.
type UpdateWikiNodeTitleRequest struct {
	Title string `json:"title"`
}

// MoveWikiNodeRequest is the body for POST .../nodes/:node_token/move.
type MoveWikiNodeRequest struct {
	TargetParentToken string `json:"target_parent_token,omitempty"`
	TargetSpaceID     string `json:"target_space_id,omitempty"`
}

// wikiNodeMutationResponse is shared by create/update/move node APIs.
type wikiNodeMutationResponse struct {
	apiResponse
	Data struct {
		Node WikiNode `json:"node"`
	} `json:"data"`
}

// --- Docx block types ---

const (
	// DocxBlockTypePage is the document page root block.
	DocxBlockTypePage = 1
	// DocxBlockTypeText is a paragraph / text block.
	DocxBlockTypeText = 2
	// DocxBlockTypeFile is a file attachment block.
	DocxBlockTypeFile = 23
	// DocxBlockTypeView wraps a File Block and controls its display mode.
	// Creating a File Block returns a View Block whose children[0] is the File Block ID.
	DocxBlockTypeView = 33
)

// DocxBlock is a document block used for create/list/update operations.
type DocxBlock struct {
	BlockID   string    `json:"block_id,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`
	Children  []string  `json:"children,omitempty"`
	BlockType int       `json:"block_type"`
	Text      *DocxText `json:"text,omitempty"`
	File      *DocxFile `json:"file,omitempty"`
}

// DocxText holds paragraph text content.
type DocxText struct {
	Elements []DocxTextElement `json:"elements"`
	Style    *DocxTextStyle    `json:"style,omitempty"`
}

// DocxTextStyle is paragraph-level style (optional empty object is fine).
type DocxTextStyle struct{}

// DocxTextElement is one rich-text element inside a text block.
type DocxTextElement struct {
	TextRun *DocxTextRun `json:"text_run,omitempty"`
}

// DocxTextRun is plain text content.
type DocxTextRun struct {
	Content string `json:"content"`
}

// DocxFile is a file block payload.
type DocxFile struct {
	Token string `json:"token"`
	Name  string `json:"name,omitempty"`
}

// CreateBlockChildrenRequest is POST .../blocks/:block_id/children.
type CreateBlockChildrenRequest struct {
	Children []DocxBlock `json:"children"`
	Index    int         `json:"index,omitempty"`
}

// CreateBlockChildrenResponse is the create-children response.
type CreateBlockChildrenResponse struct {
	apiResponse
	Data struct {
		Children []DocxBlock `json:"children"`
		DocumentRevisionID int `json:"document_revision_id"`
	} `json:"data"`
}

// ListDocumentBlocksResponse is GET .../documents/:id/blocks.
type ListDocumentBlocksResponse struct {
	apiResponse
	Data struct {
		Items     []DocxBlock `json:"items"`
		PageToken string      `json:"page_token"`
		HasMore   bool        `json:"has_more"`
	} `json:"data"`
}

// BatchDeleteBlockChildrenRequest deletes a contiguous child range.
type BatchDeleteBlockChildrenRequest struct {
	StartIndex int `json:"start_index"`
	EndIndex   int `json:"end_index"`
}

// ReplaceFileRequest is PATCH body for replacing a file block's media.
type ReplaceFileRequest struct {
	ReplaceFile *ReplaceFilePayload `json:"replace_file"`
}

// ReplaceFilePayload carries the new media token.
type ReplaceFilePayload struct {
	Token string `json:"token"`
}

// UpdateBlockResponse is the PATCH block response.
type UpdateBlockResponse struct {
	apiResponse
	Data struct {
		Block DocxBlock `json:"block"`
	} `json:"data"`
}

// --- Media upload ---

// UploadMediaParentTypeDocxFile is parent_type for file blocks in docx.
const UploadMediaParentTypeDocxFile = "docx_file"

// uploadAllMaxBytes is Feishu's documented limit for upload_all (20 MiB).
const uploadAllMaxBytes = 20 * 1024 * 1024

// maxResponseBodyBytes caps JSON API response bodies (10 MiB).
const maxResponseBodyBytes = 10 * 1024 * 1024

// UploadMediaPrepareRequest is POST /drive/v1/medias/upload_prepare.
type UploadMediaPrepareRequest struct {
	FileName   string `json:"file_name"`
	ParentType string `json:"parent_type"`
	ParentNode string `json:"parent_node"`
	Size       int64  `json:"size"`
}

// UploadMediaPrepareResponse returns multipart upload plan.
type UploadMediaPrepareResponse struct {
	apiResponse
	Data struct {
		UploadID  string `json:"upload_id"`
		BlockSize int    `json:"block_size"`
		BlockNum  int    `json:"block_num"`
	} `json:"data"`
}

// UploadMediaFinishRequest completes a multipart upload.
type UploadMediaFinishRequest struct {
	UploadID string `json:"upload_id"`
	BlockNum int    `json:"block_num"`
}

// UploadMediaFinishResponse returns the uploaded file token.
type UploadMediaFinishResponse struct {
	apiResponse
	Data struct {
		FileToken string `json:"file_token"`
	} `json:"data"`
}

// UploadMediaAllResponse is the single-shot upload_all response.
type UploadMediaAllResponse struct {
	apiResponse
	Data struct {
		FileToken string `json:"file_token"`
	} `json:"data"`
}

// NewTextBlock builds a simple text paragraph block for create-children.
func NewTextBlock(content string) DocxBlock {
	return DocxBlock{
		BlockType: DocxBlockTypeText,
		Text: &DocxText{
			Elements: []DocxTextElement{
				{TextRun: &DocxTextRun{Content: content}},
			},
			Style: &DocxTextStyle{},
		},
	}
}

// NewEmptyFileBlock builds an empty file block (token filled after upload).
func NewEmptyFileBlock() DocxBlock {
	return DocxBlock{
		BlockType: DocxBlockTypeFile,
		File:      &DocxFile{Token: ""},
	}
}
