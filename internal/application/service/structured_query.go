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

func isStructuredFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".xls", ".xlsx":
		return true
	default:
		return false
	}
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
	req.Header.Set("Idempotency-Key", knowledge.ID+"-"+knowledge.FileHash)
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
