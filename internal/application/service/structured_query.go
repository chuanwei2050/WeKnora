package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/structuredquery"
	"github.com/Tencent/WeKnora/internal/types"
)

type structuredDatasetAccepted struct {
	DatasetID string `json:"dataset_id"`
	VersionID string `json:"version_id"`
	JobID     string `json:"job_id"`
}

type knowledgeMetadataMerger interface {
	MergeKnowledgeMetadata(context.Context, uint64, string, types.JSON) error
}

var structuredMetadataKeys = []string{
	"structured_dataset_id",
	"structured_version_id",
	"structured_job_id",
	"structured_submitted_at",
}

func isStructuredFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".xls", ".xlsx":
		return true
	default:
		return false
	}
}

// cleanupStructuredDatasets drops sidecar SQL datasets for this knowledge
// (including rebuild orphans keyed as knowledgeID-*) and clears local bindings.
// Best-effort: failures are logged and do not fail the caller.
func (s *knowledgeService) cleanupStructuredDatasets(ctx context.Context, knowledge *types.Knowledge) {
	if knowledge == nil || strings.TrimSpace(knowledge.ID) == "" {
		return
	}
	cfg := s.config
	if cfg == nil || cfg.StructuredQuery == nil || !cfg.StructuredQuery.Enabled {
		stripStructuredMetadata(knowledge)
		return
	}
	sq := cfg.StructuredQuery
	if strings.TrimSpace(sq.BaseURL) == "" || strings.TrimSpace(sq.APIKey) == "" {
		stripStructuredMetadata(knowledge)
		return
	}
	client := structuredquery.Client{
		BaseURL: sq.BaseURL,
		APIKey:  sq.APIKey,
		Timeout: time.Duration(max(1, sq.RequestTimeout)) * time.Second,
	}
	prefix := knowledge.ID + "-"
	deleted, err := client.DeleteDatasetsByPrefix(ctx, knowledge.TenantID, knowledge.KnowledgeBaseID, prefix)
	if err != nil {
		logger.Warnf(ctx, "[StructuredQuery] cleanup failed knowledge=%s err=%v", knowledge.ID, err)
	} else if deleted > 0 {
		logger.Infof(ctx, "[StructuredQuery] cleaned %d dataset(s) for knowledge=%s", deleted, knowledge.ID)
	}
	stripStructuredMetadata(knowledge)
}

func stripStructuredMetadata(knowledge *types.Knowledge) {
	if knowledge == nil || len(knowledge.Metadata) == 0 {
		return
	}
	meta, err := knowledge.Metadata.Map()
	if err != nil || meta == nil {
		return
	}
	changed := false
	for _, key := range structuredMetadataKeys {
		if _, ok := meta[key]; ok {
			delete(meta, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	encoded, marshalErr := json.Marshal(meta)
	if marshalErr != nil {
		return
	}
	knowledge.Metadata = types.JSON(encoded)
}

// resubmitStructuredFile drops existing sidecar datasets for this knowledge
// (including prior rebuild orphans), then uploads again under a fresh
// idempotency key. Used by KB maintenance "structured" rebuild and
// single-document ReparseKnowledge.
func (s *knowledgeService) resubmitStructuredFile(ctx context.Context, cfg *config.StructuredQueryConfig, knowledge *types.Knowledge, fileService interface {
	GetFile(context.Context, string) (io.ReadCloser, error)
}, merger any, idempotencyKey string) (structuredDatasetAccepted, error) {
	s.cleanupStructuredDatasets(ctx, knowledge)
	return submitStructuredFileRequestWithKey(ctx, cfg, knowledge, fileService, merger, idempotencyKey)
}

func (s *knowledgeService) submitStructuredFile(ctx context.Context, knowledge *types.Knowledge, fileService interface {
	GetFile(context.Context, string) (io.ReadCloser, error)
}) {
	cfg := s.config.StructuredQuery
	if cfg == nil || !cfg.Enabled || !isStructuredFile(knowledge.FileName) {
		return
	}
	go func() {
		background := context.WithoutCancel(ctx)
		if _, err := submitStructuredFileRequest(background, cfg, knowledge, fileService, s.repo); err != nil {
			logger.Errorf(background, "[StructuredQuery] dataset submission failed: %v", err)
		}
	}()
}

// queueStructuredResubmit deletes old sidecar datasets and re-uploads under a
// fresh key. Used by single-doc / batch reparse so orphans are not retained.
func (s *knowledgeService) queueStructuredResubmit(ctx context.Context, knowledge *types.Knowledge, fileService interface {
	GetFile(context.Context, string) (io.ReadCloser, error)
}, idempotencyKey string) {
	cfg := s.config.StructuredQuery
	if cfg == nil || !cfg.Enabled || knowledge == nil || !isStructuredFile(knowledge.FileName) {
		return
	}
	go func() {
		background := context.WithoutCancel(ctx)
		if _, err := s.resubmitStructuredFile(background, cfg, knowledge, fileService, s.repo, idempotencyKey); err != nil {
			logger.Errorf(background, "[StructuredQuery] dataset resubmit failed: %v", err)
		}
	}()
}

// BackfillStructuredFile synchronously submits one historical table document.
// It is intentionally exposed through a narrow optional interface used by the
// one-shot command, without expanding the public KnowledgeService contract.
func (s *knowledgeService) BackfillStructuredFile(ctx context.Context, knowledge *types.Knowledge) (string, error) {
	if knowledge == nil {
		return "", fmt.Errorf("knowledge is required")
	}
	cfg := s.config.StructuredQuery
	if cfg == nil || !cfg.Enabled {
		return "", fmt.Errorf("structured query is disabled")
	}
	if !isStructuredFile(knowledge.FileName) {
		return "", fmt.Errorf("unsupported structured file: %s", knowledge.FileName)
	}
	if knowledge.GetMetadata()["structured_dataset_id"] != "" {
		return knowledge.GetMetadata()["structured_job_id"], nil
	}
	tenant, err := s.tenantService.GetTenantByID(ctx, knowledge.TenantID)
	if err != nil {
		return "", fmt.Errorf("load tenant %d: %w", knowledge.TenantID, err)
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, knowledge.TenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	if err != nil {
		return "", fmt.Errorf("load knowledge base %s: %w", knowledge.KnowledgeBaseID, err)
	}
	accepted, err := submitStructuredFileRequest(ctx, cfg, knowledge, s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath), s.repo)
	return accepted.JobID, err
}

func submitStructuredFileRequest(ctx context.Context, cfg *config.StructuredQueryConfig, knowledge *types.Knowledge, fileService interface {
	GetFile(context.Context, string) (io.ReadCloser, error)
}, merger any) (structuredDatasetAccepted, error) {
	return submitStructuredFileRequestWithKey(ctx, cfg, knowledge, fileService, merger, knowledge.ID+"-"+knowledge.FileHash)
}

func submitStructuredFileRequestWithKey(ctx context.Context, cfg *config.StructuredQueryConfig, knowledge *types.Knowledge, fileService interface {
	GetFile(context.Context, string) (io.ReadCloser, error)
}, merger any, idempotencyKey string) (structuredDatasetAccepted, error) {
	reader, err := fileService.GetFile(ctx, knowledge.FilePath)
	if err != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("open upload: %w", err)
	}
	defer reader.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("namespace", knowledge.KnowledgeBaseID)
	part, err := writer.CreateFormFile("file", knowledge.FileName)
	if err == nil {
		_, err = io.Copy(part, reader)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("build upload: %w", err)
	}
	timeout := time.Duration(max(1, cfg.RequestTimeout)) * time.Second
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/v1/datasets/files", &body)
	if err != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-API-Key", cfg.APIKey)
	req.Header.Set("X-Tenant-ID", strconv.FormatUint(knowledge.TenantID, 10))
	req.Header.Set("Idempotency-Key", idempotencyKey)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return structuredDatasetAccepted{}, fmt.Errorf("upload status=%d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var accepted structuredDatasetAccepted
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&accepted); err != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("decode upload response: %w", err)
	}
	metadata := map[string]string{}
	metadata["structured_dataset_id"] = accepted.DatasetID
	metadata["structured_version_id"] = accepted.VersionID
	metadata["structured_job_id"] = accepted.JobID
	metadata["structured_submitted_at"] = strconv.FormatInt(time.Now().Unix(), 10)
	encoded, marshalErr := json.Marshal(metadata)
	if marshalErr != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("encode identifiers: %w", marshalErr)
	}
	metadataMerger, ok := merger.(knowledgeMetadataMerger)
	if !ok {
		return structuredDatasetAccepted{}, fmt.Errorf("repository does not support atomic metadata merge")
	}
	if updateErr := metadataMerger.MergeKnowledgeMetadata(ctx, knowledge.TenantID, knowledge.ID, types.JSON(encoded)); updateErr != nil {
		return structuredDatasetAccepted{}, fmt.Errorf("persist identifiers: %w", updateErr)
	}
	logger.Infof(ctx, "[StructuredQuery] dataset submitted knowledge_id=%s job_id=%s", knowledge.ID, accepted.JobID)
	return accepted, nil
}
