package types

import (
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

// IsFeishuKnowledgeSyncEnabled reports whether ENABLE_FEISHU_KNOWLEDGE_SYNC is "true" (case-insensitive).
func IsFeishuKnowledgeSyncEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(EnvEnableFeishuKnowledgeSync)), "true")
}

// Feishu publish feature constants.
const (
	// EnvEnableFeishuKnowledgeSync gates routes, UI and task consumption.
	EnvEnableFeishuKnowledgeSync = "ENABLE_FEISHU_KNOWLEDGE_SYNC"

	FeishuPublishSourceKindHome          = "managed_home"
	FeishuPublishSourceKindArchive       = "archive"
	FeishuPublishSourceKindVirtualFolder = "virtual_folder"
	FeishuPublishSourceKindTag           = "tag"
	FeishuPublishSourceKindDirectory     = "directory"
	FeishuPublishSourceKindKnowledge     = "knowledge"

	// Stable source IDs for virtual left-rail containers.
	FeishuPublishVirtualPublicID         = "public"
	FeishuPublishVirtualUncategorizedID  = "uncategorized"
	FeishuPublishVirtualUncategorizedTitle = "未分类"

	FeishuPublishMappingActive   = "active"
	FeishuPublishMappingArchived = "archived"
	FeishuPublishMappingConflict = "conflict"

	FeishuPublishRunQueued    = "queued"
	FeishuPublishRunRunning   = "running"
	FeishuPublishRunPartial   = "partial"
	FeishuPublishRunSucceeded = "succeeded"
	FeishuPublishRunFailed    = "failed"

	FeishuPublishOpCreate         = "create"
	FeishuPublishOpReplaceContent = "replace_content"
	FeishuPublishOpRename         = "rename"
	FeishuPublishOpMove           = "move"
	FeishuPublishOpRetire         = "retire"
	FeishuPublishOpRestore        = "restore"
	FeishuPublishOpSkip           = "skip"
	FeishuPublishOpConflict       = "conflict"
	FeishuPublishOpBlocked        = "blocked"

	FeishuPublishEligibilityPresent               = "present"
	FeishuPublishEligibilityDeleted               = "deleted"
	FeishuPublishEligibilityTemporarilyIneligible = "temporarily_ineligible"

	FeishuPublishConnectionUnknown = "unknown"
	FeishuPublishConnectionOK      = "ok"
	FeishuPublishConnectionFailed  = "failed"

	// FeishuPublishMaxBodyBlocks limits searchable body blocks per knowledge page.
	FeishuPublishMaxBodyBlocks = 40
	// FeishuPublishMaxBodyChars limits searchable body text length.
	FeishuPublishMaxBodyChars = 20000
	// FeishuPublishMaxUploadBytes is the soft product ceiling for a single attachment.
	// Feishu multipart media upload supports large payloads (tenant-plan dependent, commonly up to ~2GiB);
	// keep a hard product ceiling to bound abuse while covering large office packages.
	FeishuPublishMaxUploadBytes int64 = 2 * 1024 * 1024 * 1024
	// FeishuPublishMaxUploadMB is FeishuPublishMaxUploadBytes expressed in mebibytes (for messages/UI).
	FeishuPublishMaxUploadMB = FeishuPublishMaxUploadBytes / (1024 * 1024)

	FeishuPublishArchiveTitle = "_已归档"
	// FeishuPublishHomeMarker is a stable technical source marker embedded in managed
	// Feishu page bodies; keep the historical value so existing pages remain identifiable.
	FeishuPublishHomeMarker = "weknora-feishu-publish"
)

// FeishuPublishConfig stores encrypted Feishu credentials and the locked publish target.
// Secrets are never returned in API responses; use AppSecretConfigured instead.
type FeishuPublishConfig struct {
	ID                   string         `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID             uint64         `json:"tenant_id"`
	KnowledgeBaseID      string         `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	TargetID             string         `json:"target_id" gorm:"type:varchar(36);index"`
	AppID                string         `json:"app_id" gorm:"type:varchar(255)"`
	AppSecretCipher      string         `json:"-" gorm:"type:text"`
	AppSecretConfigured  bool           `json:"app_secret_configured" gorm:"-"`
	SpaceID              string         `json:"space_id" gorm:"type:varchar(128)"`
	SpaceName            string         `json:"space_name" gorm:"type:varchar(512)"`
	ParentNodeToken      string         `json:"parent_node_token" gorm:"type:varchar(128)"`
	ParentPath           JSON           `json:"parent_path" gorm:"type:json"`
	SpaceLocked          bool           `json:"space_locked"`
	ConnectionStatus     string         `json:"connection_status" gorm:"type:varchar(32)"`
	LastConnectionError  string         `json:"last_connection_error,omitempty" gorm:"type:varchar(512)"`
	LastSuccessAt        *time.Time     `json:"last_success_at,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `json:"-" gorm:"index"`
}

// TableName returns the database table name.
func (FeishuPublishConfig) TableName() string { return "feishu_publish_configs" }

// FeishuPublishSnapshot is an immutable source snapshot used by a run.
type FeishuPublishSnapshot struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36)"`
	TargetID        string    `json:"target_id" gorm:"type:varchar(36)"`
	Digest          string    `json:"digest" gorm:"type:varchar(128)"`
	Payload         JSON      `json:"payload" gorm:"type:json"`
	CreatedAt       time.Time `json:"created_at"`
}

// TableName returns the database table name.
func (FeishuPublishSnapshot) TableName() string { return "feishu_publish_snapshots" }

// FeishuPublishRun tracks one queued/executed publish attempt.
type FeishuPublishRun struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id"`
	KnowledgeBaseID string     `json:"knowledge_base_id" gorm:"type:varchar(36)"`
	TargetID        string     `json:"target_id" gorm:"type:varchar(36)"`
	ConfigID        string     `json:"config_id" gorm:"type:varchar(36)"`
	SnapshotID      string     `json:"snapshot_id" gorm:"type:varchar(36)"`
	SnapshotDigest  string     `json:"snapshot_digest" gorm:"type:varchar(128)"`
	SpaceID         string     `json:"space_id" gorm:"type:varchar(128)"`
	ParentNodeToken string     `json:"parent_node_token" gorm:"type:varchar(128)"`
	Status          string     `json:"status" gorm:"type:varchar(32)"`
	Stage           string     `json:"stage" gorm:"type:varchar(64)"`
	ProgressDone    int        `json:"progress_done" gorm:"default:0"`
	ProgressTotal   int        `json:"progress_total" gorm:"default:0"`
	ProgressLabel   string     `json:"progress_label,omitempty" gorm:"type:varchar(512)"`
	ConfigRevision  JSON       `json:"config_revision" gorm:"type:json"`
	Counts          JSON       `json:"counts" gorm:"type:json"`
	ItemResults     JSON       `json:"item_results" gorm:"type:json"`
	ErrorCode       string     `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	ErrorSummary    string     `json:"error_summary,omitempty" gorm:"type:varchar(512)"`
	FeishuHomeURL   string     `json:"feishu_home_url,omitempty" gorm:"type:varchar(1024)"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// TableName returns the database table name.
func (FeishuPublishRun) TableName() string { return "feishu_publish_runs" }

// FeishuPublishMapping stores stable WeKnora source → Feishu remote object links.
type FeishuPublishMapping struct {
	ID               string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64    `json:"tenant_id"`
	KnowledgeBaseID  string    `json:"knowledge_base_id" gorm:"type:varchar(36)"`
	TargetID         string    `json:"target_id" gorm:"type:varchar(36)"`
	SourceKind       string    `json:"source_kind" gorm:"type:varchar(32)"`
	SourceID         string    `json:"source_id" gorm:"type:varchar(36)"`
	SpaceID          string    `json:"space_id" gorm:"type:varchar(128)"`
	NodeToken        string    `json:"node_token" gorm:"type:varchar(128)"`
	ObjToken         string    `json:"obj_token" gorm:"type:varchar(128)"`
	ParentNodeToken  string    `json:"parent_node_token" gorm:"type:varchar(128)"`
	Title            string    `json:"title" gorm:"type:varchar(512)"`
	MetaBlockIDs     JSON      `json:"meta_block_ids" gorm:"type:json"`
	BodyBlockIDs     JSON      `json:"body_block_ids" gorm:"type:json"`
	FileBlockID      string    `json:"file_block_id" gorm:"type:varchar(128)"`
	MediaToken       string    `json:"media_token" gorm:"type:varchar(128)"`
	SourceHash       string    `json:"source_hash" gorm:"type:varchar(128)"`
	BodyHash         string    `json:"body_hash" gorm:"type:varchar(128)"`
	SourceVersion    string    `json:"source_version" gorm:"type:varchar(128)"`
	Status           string    `json:"status" gorm:"type:varchar(32)"`
	LastSuccessRunID string    `json:"last_success_run_id" gorm:"type:varchar(36)"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TableName returns the database table name.
func (FeishuPublishMapping) TableName() string { return "feishu_publish_mappings" }

// --- Snapshot / plan DTOs (not persisted as rows except inside snapshot.payload) ---

// FeishuPublishPathNode is a breadcrumb segment for UI display.
type FeishuPublishPathNode struct {
	NodeToken string `json:"node_token,omitempty"`
	Title     string `json:"title"`
}

// FeishuPublishSnapshotPayload is the typed content of FeishuPublishSnapshot.Payload.
type FeishuPublishSnapshotPayload struct {
	KnowledgeBaseName string                       `json:"knowledge_base_name,omitempty"`
	VirtualFolders    []FeishuPublishNavSnap       `json:"virtual_folders,omitempty"`
	Tags              []FeishuPublishNavSnap       `json:"tags,omitempty"`
	Directories       []FeishuPublishDirectorySnap `json:"directories"`
	Knowledge         []FeishuPublishKnowledgeSnap `json:"knowledge"`
	MappedIDs         []FeishuPublishMappedSource  `json:"mapped_ids"`
}

// FeishuPublishNavSnap captures a navigational wiki node (virtual folder or tag).
type FeishuPublishNavSnap struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ExpectedParentKind string `json:"expected_parent_kind,omitempty"`
	ExpectedParentID   string `json:"expected_parent_id,omitempty"`
}

// FeishuPublishDirectorySnap captures one directory for planning.
type FeishuPublishDirectorySnap struct {
	ID                 string `json:"id"`
	ParentID           string `json:"parent_id,omitempty"`
	TagID              string `json:"tag_id,omitempty"`
	Name               string `json:"name"`
	ExpectedParentKind string `json:"expected_parent_kind,omitempty"`
	ExpectedParentID   string `json:"expected_parent_id,omitempty"`
	// ExpectedParent is deprecated; kept for older in-flight snapshots (directory id or "").
	ExpectedParent string `json:"expected_parent,omitempty"`
}

// FeishuPublishKnowledgeSnap captures one publishable or tracked knowledge item.
type FeishuPublishKnowledgeSnap struct {
	ID                 string `json:"id"`
	DirectoryID        string `json:"directory_id,omitempty"`
	TagID              string `json:"tag_id,omitempty"`
	Title              string `json:"title"`
	FileName           string `json:"file_name"`
	MIME               string `json:"mime"`
	Length             int64  `json:"length"`
	ObjectKey          string `json:"object_key"`
	ObjectVersion      string `json:"object_version"`
	FileHash           string `json:"file_hash"`
	BodyHash           string `json:"body_hash"`
	BodyText           string `json:"body_text,omitempty"`
	BodyTruncated      bool   `json:"body_truncated"`
	SourceVersion      string `json:"source_version"`
	Eligibility        string `json:"eligibility"`
	SkipReason         string `json:"skip_reason,omitempty"`
	ExpectedParentKind string `json:"expected_parent_kind,omitempty"`
	ExpectedParentID   string `json:"expected_parent_id,omitempty"`
}

// FeishuPublishMappedSource records previously mapped source identity for retire/restore planning.
type FeishuPublishMappedSource struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Status     string `json:"status"`
}

// FeishuPublishPlanOp is one planned remote operation.
type FeishuPublishPlanOp struct {
	Op         string `json:"op"`
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	BlockedBy  string `json:"blocked_by,omitempty"`
	UploadBytes int64 `json:"upload_bytes,omitempty"`
}

// FeishuPublishCounts aggregates preview/run counters.
type FeishuPublishCounts struct {
	Create   int   `json:"create"`
	Update   int   `json:"update"`
	Move     int   `json:"move"`
	Retire   int   `json:"retire"`
	Restore  int   `json:"restore"`
	Skip     int   `json:"skip"`
	Conflict int   `json:"conflict"`
	Blocked  int   `json:"blocked"`
	Failed   int   `json:"failed"`
	UploadBytes int64 `json:"upload_bytes"`
}

// FeishuPublishPreview is returned by preview/confirm APIs.
type FeishuPublishPreview struct {
	Digest      string                 `json:"digest"`
	Counts      FeishuPublishCounts    `json:"counts"`
	Operations  []FeishuPublishPlanOp  `json:"operations"`
	Warnings    []string               `json:"warnings,omitempty"`
	SpaceID     string                 `json:"space_id"`
	SpaceName   string                 `json:"space_name"`
	ParentToken string                 `json:"parent_node_token"`
	ParentPath  []FeishuPublishPathNode `json:"parent_path,omitempty"`
}

// FeishuPublishItemResult records one executed item outcome.
type FeishuPublishItemResult struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Op         string `json:"op"`
	Status     string `json:"status"` // succeeded | failed | skipped | conflict | blocked
	ErrorCode  string `json:"error_code,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

// FeishuPublishConfigRevision is the frozen credential/target copy stored on a run.
type FeishuPublishConfigRevision struct {
	AppID           string `json:"app_id"`
	AppSecretCipher string `json:"app_secret_cipher"`
	SpaceID         string `json:"space_id"`
	SpaceName       string `json:"space_name"`
	ParentNodeToken string `json:"parent_node_token"`
}

// --- API request/response DTOs ---

// FeishuPublishConfigView is the safe read model for clients.
type FeishuPublishConfigView struct {
	Configured          bool                       `json:"configured"`
	AppID               string                     `json:"app_id,omitempty"`
	AppSecretConfigured bool                       `json:"app_secret_configured"`
	SpaceID             string                     `json:"space_id,omitempty"`
	SpaceName           string                     `json:"space_name,omitempty"`
	ParentNodeToken     string                     `json:"parent_node_token,omitempty"`
	ParentPath          []FeishuPublishPathNode    `json:"parent_path,omitempty"`
	SpaceLocked         bool                       `json:"space_locked"`
	ConnectionStatus    string                     `json:"connection_status"`
	LastConnectionError string                     `json:"last_connection_error,omitempty"`
	LastSuccessAt       *time.Time                 `json:"last_success_at,omitempty"`
	LatestRun           *FeishuPublishRun          `json:"latest_run,omitempty"`
	HostedSummary       *FeishuPublishHostedSummary `json:"hosted_summary,omitempty"`
	FeatureEnabled      bool                       `json:"feature_enabled"`
}

// FeishuPublishHostedSummary counts currently active managed Feishu nodes for the KB.
type FeishuPublishHostedSummary struct {
	Documents   int `json:"documents"`
	Directories int `json:"directories"`
	Tags        int `json:"tags"`
	TotalActive int `json:"total_active"`
}

// FeishuPublishDiscoverSpacesRequest uses current form credentials without persistence.
type FeishuPublishDiscoverSpacesRequest struct {
	AppID     string `json:"app_id" binding:"required"`
	AppSecret string `json:"app_secret"`
	PageToken string `json:"page_token,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
}

// FeishuPublishSpaceItem is one discoverable wiki space.
type FeishuPublishSpaceItem struct {
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
}

// FeishuPublishDiscoverSpacesResponse is a typed space page.
type FeishuPublishDiscoverSpacesResponse struct {
	Items     []FeishuPublishSpaceItem `json:"items"`
	HasMore   bool                     `json:"has_more"`
	PageToken string                   `json:"page_token,omitempty"`
}

// FeishuPublishListNodesRequest loads a wiki page tree page with live credentials.
type FeishuPublishListNodesRequest struct {
	AppID           string `json:"app_id" binding:"required"`
	AppSecret       string `json:"app_secret"`
	SpaceID         string `json:"space_id" binding:"required"`
	ParentNodeToken string `json:"parent_node_token,omitempty"`
	PageToken       string `json:"page_token,omitempty"`
	PageSize        int    `json:"page_size,omitempty"`
}

// FeishuPublishNodeItem is one wiki node option for the location picker.
type FeishuPublishNodeItem struct {
	NodeToken string `json:"node_token"`
	Title     string `json:"title"`
	HasChild  bool   `json:"has_child"`
	ObjType   string `json:"obj_type,omitempty"`
	Path      []FeishuPublishPathNode `json:"path,omitempty"`
}

// FeishuPublishListNodesResponse is a typed node page.
type FeishuPublishListNodesResponse struct {
	Items     []FeishuPublishNodeItem `json:"items"`
	HasMore   bool                    `json:"has_more"`
	PageToken string                  `json:"page_token,omitempty"`
}

// FeishuPublishSyncRequest is shared by preview and confirm.
type FeishuPublishSyncRequest struct {
	AppID           string                  `json:"app_id" binding:"required"`
	AppSecret       string                  `json:"app_secret"`
	SpaceID         string                  `json:"space_id" binding:"required"`
	SpaceName       string                  `json:"space_name,omitempty"`
	ParentNodeToken string                  `json:"parent_node_token,omitempty"`
	ParentPath      []FeishuPublishPathNode `json:"parent_path,omitempty"`
	// PreviewDigest is required on confirm; empty on preview.
	PreviewDigest string `json:"preview_digest,omitempty"`
	// ConfirmSpaceMove acknowledges same-space parent relocation after space lock.
	ConfirmSpaceMove bool `json:"confirm_space_move,omitempty"`
}

// FeishuPublishConfirmResponse is returned when a run is queued or impact changed.
type FeishuPublishConfirmResponse struct {
	Accepted bool                  `json:"accepted"`
	Run      *FeishuPublishRun     `json:"run,omitempty"`
	Preview  *FeishuPublishPreview `json:"preview,omitempty"`
	Message  string                `json:"message,omitempty"`
}

// FeishuPublishError is a typed API error body without secrets.
type FeishuPublishError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}
