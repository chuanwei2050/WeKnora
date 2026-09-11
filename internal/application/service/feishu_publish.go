package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	feishuconn "github.com/Tencent/WeKnora/internal/datasource/connector/feishu"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

var (
	errFeishuPublishDisabled    = errors.New("feishu knowledge sync is disabled")
	errFeishuPublishForbidden   = errors.New("feishu publish access denied")
	errFeishuPublishKBType      = errors.New("feishu publish requires a document knowledge base")
	errFeishuPublishSecretReq   = errors.New("app_secret is required when app_id changes or no secret is stored")
	errFeishuPublishLoopOverlap = errors.New("publish target overlaps an existing feishu data source")
	errFeishuPublishSpaceLocked = errors.New("space is locked; unbind before switching spaces")
	errFeishuPublishActiveRun   = errors.New("an active publish run already exists for this target")
	errFeishuPublishParentMiss  = errors.New("parent node does not belong to the selected space")
	errFeishuPublishEndpoint    = errors.New("custom feishu endpoint is not allowed")
)

// Exported sentinels for HTTP mapping.
var (
	ErrFeishuPublishDisabled    = errFeishuPublishDisabled
	ErrFeishuPublishForbidden   = errFeishuPublishForbidden
	ErrFeishuPublishKBType      = errFeishuPublishKBType
	ErrFeishuPublishSecretReq   = errFeishuPublishSecretReq
	ErrFeishuPublishLoopOverlap = errFeishuPublishLoopOverlap
	ErrFeishuPublishSpaceLocked = errFeishuPublishSpaceLocked
	ErrFeishuPublishActiveRun   = errFeishuPublishActiveRun
	ErrFeishuPublishParentMiss  = errFeishuPublishParentMiss
	ErrFeishuPublishEndpoint    = errFeishuPublishEndpoint
)

// FeishuPublishService implements interfaces.FeishuPublishService.
type FeishuPublishService struct {
	repo          interfaces.FeishuPublishRepository
	kbService     interfaces.KnowledgeBaseService
	knowledgeRepo interfaces.KnowledgeRepository
	directoryRepo interfaces.KnowledgeDirectoryRepository
	tagRepo       interfaces.KnowledgeTagRepository
	chunkRepo     interfaces.ChunkRepository
	dsRepo        interfaces.DataSourceRepository
	fileSvc       interfaces.FileService
	taskEnqueuer  interfaces.TaskEnqueuer
}

// NewFeishuPublishService constructs the Feishu publish orchestration service.
func NewFeishuPublishService(
	repo interfaces.FeishuPublishRepository,
	kbService interfaces.KnowledgeBaseService,
	knowledgeRepo interfaces.KnowledgeRepository,
	directoryRepo interfaces.KnowledgeDirectoryRepository,
	tagRepo interfaces.KnowledgeTagRepository,
	chunkRepo interfaces.ChunkRepository,
	dsRepo interfaces.DataSourceRepository,
	fileSvc interfaces.FileService,
	taskEnqueuer interfaces.TaskEnqueuer,
) interfaces.FeishuPublishService {
	return &FeishuPublishService{
		repo:          repo,
		kbService:     kbService,
		knowledgeRepo: knowledgeRepo,
		directoryRepo: directoryRepo,
		tagRepo:       tagRepo,
		chunkRepo:     chunkRepo,
		dsRepo:        dsRepo,
		fileSvc:       fileSvc,
		taskEnqueuer:  taskEnqueuer,
	}
}

// IsEnabled reports whether the feature flag is on.
func (s *FeishuPublishService) IsEnabled() bool {
	return types.IsFeishuKnowledgeSyncEnabled()
}

func (s *FeishuPublishService) requireEnabled() error {
	if !s.IsEnabled() {
		return errFeishuPublishDisabled
	}
	return nil
}

func (s *FeishuPublishService) authorizeKB(ctx context.Context, tenantID uint64, kbID string) (*types.KnowledgeBase, error) {
	if err := s.requireEnabled(); err != nil {
		return nil, err
	}
	if kbID == "" {
		return nil, errors.New("knowledge base id is required")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, errFeishuPublishForbidden
	}
	if kb.TenantID != tenantID {
		return nil, errFeishuPublishForbidden
	}
	if !types.CanManageKnowledgeBase(ctx, kb) {
		return nil, errFeishuPublishForbidden
	}
	if kb.Type != "" && kb.Type != types.KnowledgeBaseTypeDocument {
		return nil, errFeishuPublishKBType
	}
	return kb, nil
}

// GetConfigView returns the safe read model for the UI.
func (s *FeishuPublishService) GetConfigView(ctx context.Context, tenantID uint64, kbID string) (*types.FeishuPublishConfigView, error) {
	if _, err := s.authorizeKB(ctx, tenantID, kbID); err != nil {
		return nil, err
	}
	view := &types.FeishuPublishConfigView{
		FeatureEnabled:   s.IsEnabled(),
		ConnectionStatus: types.FeishuPublishConnectionUnknown,
	}
	cfg, err := s.repo.GetConfigByKB(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return view, nil
	}
	view.Configured = true
	view.AppID = cfg.AppID
	view.AppSecretConfigured = cfg.AppSecretConfigured
	view.SpaceID = cfg.SpaceID
	view.SpaceName = cfg.SpaceName
	view.ParentNodeToken = cfg.ParentNodeToken
	view.SpaceLocked = cfg.SpaceLocked
	view.ConnectionStatus = cfg.ConnectionStatus
	view.LastConnectionError = cfg.LastConnectionError
	view.LastSuccessAt = cfg.LastSuccessAt
	_ = json.Unmarshal(cfg.ParentPath, &view.ParentPath)

	runs, err := s.repo.ListRunsByKB(ctx, tenantID, kbID, 1)
	if err != nil {
		return nil, err
	}
	if len(runs) > 0 {
		view.LatestRun = runs[0]
	}
	if cfg.TargetID != "" {
		view.HostedSummary = s.buildHostedSummary(ctx, tenantID, kbID, cfg.TargetID)
	}
	return view, nil
}

func (s *FeishuPublishService) buildHostedSummary(ctx context.Context, tenantID uint64, kbID, targetID string) *types.FeishuPublishHostedSummary {
	mappings, err := s.repo.ListMappings(ctx, tenantID, kbID, targetID)
	if err != nil || len(mappings) == 0 {
		return &types.FeishuPublishHostedSummary{}
	}
	summary := &types.FeishuPublishHostedSummary{}
	for _, m := range mappings {
		if m == nil || m.Status != types.FeishuPublishMappingActive {
			continue
		}
		summary.TotalActive++
		switch m.SourceKind {
		case types.FeishuPublishSourceKindKnowledge:
			summary.Documents++
		case types.FeishuPublishSourceKindDirectory:
			summary.Directories++
		case types.FeishuPublishSourceKindTag:
			summary.Tags++
		}
	}
	return summary
}

func (s *FeishuPublishService) resolveSecret(ctx context.Context, tenantID uint64, kbID, appID, appSecret string) (string, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" {
		return "", errors.New("app_id is required")
	}
	if appSecret != "" {
		return appSecret, nil
	}
	cfg, err := s.repo.GetConfigByKB(ctx, tenantID, kbID)
	if err != nil {
		return "", err
	}
	if cfg == nil || cfg.AppID != appID || strings.TrimSpace(cfg.AppSecretCipher) == "" {
		return "", errFeishuPublishSecretReq
	}
	plain, err := utils.DecryptAESGCM(cfg.AppSecretCipher, utils.GetAESKey())
	if err != nil {
		return "", fmt.Errorf("decrypt app_secret: %w", err)
	}
	if plain == "" {
		return "", errFeishuPublishSecretReq
	}
	return plain, nil
}

func (s *FeishuPublishService) newOfficialClient(appID, appSecret string) *feishuconn.Client {
	return feishuconn.NewOfficialClient(appID, appSecret)
}

// DiscoverSpaces lists wiki spaces using current form credentials without persistence.
func (s *FeishuPublishService) DiscoverSpaces(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishDiscoverSpacesRequest) (*types.FeishuPublishDiscoverSpacesResponse, error) {
	if _, err := s.authorizeKB(ctx, tenantID, kbID); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("request is required")
	}
	secret, err := s.resolveSecret(ctx, tenantID, kbID, req.AppID, req.AppSecret)
	if err != nil {
		return nil, err
	}
	client := s.newOfficialClient(strings.TrimSpace(req.AppID), secret)
	spaces, hasMore, next, err := client.ListWikiSpacesPage(ctx, req.PageToken, req.PageSize)
	if err != nil {
		return nil, sanitizeFeishuErr(err)
	}
	out := &types.FeishuPublishDiscoverSpacesResponse{
		Items:     make([]types.FeishuPublishSpaceItem, 0, len(spaces)),
		HasMore:   hasMore,
		PageToken: next,
	}
	for _, sp := range spaces {
		out.Items = append(out.Items, types.FeishuPublishSpaceItem{
			SpaceID:     sp.SpaceID,
			Name:        sp.Name,
			Description: sp.Description,
			Visibility:  sp.Visibility,
		})
	}
	return out, nil
}

// ListNodes lazily loads wiki page tree nodes with live credentials.
func (s *FeishuPublishService) ListNodes(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishListNodesRequest) (*types.FeishuPublishListNodesResponse, error) {
	if _, err := s.authorizeKB(ctx, tenantID, kbID); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.New("request is required")
	}
	secret, err := s.resolveSecret(ctx, tenantID, kbID, req.AppID, req.AppSecret)
	if err != nil {
		return nil, err
	}
	client := s.newOfficialClient(strings.TrimSpace(req.AppID), secret)

	if req.ParentNodeToken != "" {
		node, err := client.GetWikiNode(ctx, req.ParentNodeToken)
		if err != nil {
			return nil, sanitizeFeishuErr(err)
		}
		if node.SpaceID != "" && node.SpaceID != req.SpaceID {
			return nil, errFeishuPublishParentMiss
		}
	}

	nodes, hasMore, next, err := client.ListWikiNodesPage(ctx, req.SpaceID, req.ParentNodeToken, req.PageToken, req.PageSize)
	if err != nil {
		return nil, sanitizeFeishuErr(err)
	}
	out := &types.FeishuPublishListNodesResponse{
		Items:     make([]types.FeishuPublishNodeItem, 0, len(nodes)),
		HasMore:   hasMore,
		PageToken: next,
	}
	for _, n := range nodes {
		out.Items = append(out.Items, types.FeishuPublishNodeItem{
			NodeToken: n.NodeToken,
			Title:     n.Title,
			HasChild:  n.HasChild,
			ObjType:   n.ObjType,
		})
	}
	return out, nil
}

func (s *FeishuPublishService) assertNoLoop(ctx context.Context, tenantID uint64, kbID, spaceID string) error {
	list, err := s.dsRepo.FindByKnowledgeBase(ctx, kbID)
	if err != nil {
		return err
	}
	for _, ds := range list {
		if ds == nil || ds.TenantID != tenantID || ds.Type != types.ConnectorTypeFeishu {
			continue
		}
		cfg, err := ds.ParseConfig()
		if err != nil || cfg == nil {
			continue
		}
		for _, rid := range cfg.ResourceIDs {
			if rid == spaceID {
				return errFeishuPublishLoopOverlap
			}
		}
	}
	return nil
}

func (s *FeishuPublishService) validateTarget(ctx context.Context, client *feishuconn.Client, spaceID, parentToken string) (spaceName string, err error) {
	pageToken := ""
	for {
		spaces, hasMore, next, err := client.ListWikiSpacesPage(ctx, pageToken, 50)
		if err != nil {
			return "", sanitizeFeishuErr(err)
		}
		for _, sp := range spaces {
			if sp.SpaceID == spaceID {
				spaceName = sp.Name
				if parentToken != "" {
					node, err := client.GetWikiNode(ctx, parentToken)
					if err != nil {
						return "", sanitizeFeishuErr(err)
					}
					if node.SpaceID != "" && node.SpaceID != spaceID {
						return "", errFeishuPublishParentMiss
					}
				}
				return spaceName, nil
			}
		}
		if !hasMore || next == "" {
			break
		}
		pageToken = next
	}
	return "", errors.New("target wiki space is not accessible")
}

func (s *FeishuPublishService) buildPreview(ctx context.Context, tenantID uint64, kb *types.KnowledgeBase, req *types.FeishuPublishSyncRequest, secret string) (*types.FeishuPublishPreview, *types.FeishuPublishSnapshotPayload, string, error) {
	if req == nil {
		return nil, nil, "", errors.New("request is required")
	}
	if strings.Contains(strings.ToLower(req.AppID), "http://") || strings.Contains(strings.ToLower(req.AppID), "https://") {
		return nil, nil, "", errFeishuPublishEndpoint
	}
	spaceID := strings.TrimSpace(req.SpaceID)
	if spaceID == "" {
		return nil, nil, "", errors.New("space_id is required")
	}
	if err := s.assertNoLoop(ctx, tenantID, kb.ID, spaceID); err != nil {
		return nil, nil, "", err
	}

	existing, err := s.repo.GetConfigByKB(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, nil, "", err
	}
	if existing != nil && existing.SpaceLocked && existing.SpaceID != "" && existing.SpaceID != spaceID {
		return nil, nil, "", errFeishuPublishSpaceLocked
	}
	if existing != nil && existing.SpaceLocked && existing.SpaceID == spaceID &&
		existing.ParentNodeToken != strings.TrimSpace(req.ParentNodeToken) && !req.ConfirmSpaceMove {
		return nil, nil, "", errors.New("confirm_space_move is required to relocate managed home within the same space")
	}

	client := s.newOfficialClient(strings.TrimSpace(req.AppID), secret)
	spaceName, err := s.validateTarget(ctx, client, spaceID, strings.TrimSpace(req.ParentNodeToken))
	if err != nil {
		return nil, nil, "", err
	}
	if req.SpaceName != "" {
		spaceName = req.SpaceName
	}

	targetID := ""
	if existing != nil {
		targetID = existing.TargetID
	}
	if targetID == "" {
		targetID = uuid.NewString()
	}

	dirs, err := s.directoryRepo.ListActiveByKB(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, nil, "", err
	}
	tags, err := s.listAllTagsForPublish(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, nil, "", err
	}
	knowledge, err := s.knowledgeRepo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID)
	if err != nil {
		return nil, nil, "", err
	}

	mappings, err := s.repo.ListMappings(ctx, tenantID, kb.ID, targetID)
	if err != nil {
		return nil, nil, "", err
	}
	s.refreshMappingsFromRemote(ctx, client, mappings)

	mapped := make([]*types.FeishuPublishMappedSource, 0, len(mappings))
	for _, m := range mappings {
		mapped = append(mapped, &types.FeishuPublishMappedSource{
			SourceKind: m.SourceKind,
			SourceID:   m.SourceID,
			Status:     m.Status,
		})
	}

	bodyByID := map[string]string{}
	for _, k := range knowledge {
		if k == nil || k.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		body, ok := s.loadFeishuSearchableBody(ctx, tenantID, k.ID)
		if !ok {
			continue
		}
		bodyByID[k.ID] = body
	}

	payload, digest, err := BuildSnapshotPayload(kb.Name, tags, dirs, knowledge, mapped, bodyByID)
	if err != nil {
		return nil, nil, "", err
	}
	ops, counts := PlanFeishuPublish(payload, mappings)
	preview := &types.FeishuPublishPreview{
		Digest:      digest,
		Counts:      counts,
		Operations:  ops,
		SpaceID:     spaceID,
		SpaceName:   spaceName,
		ParentToken: strings.TrimSpace(req.ParentNodeToken),
		ParentPath:  req.ParentPath,
	}
	for _, op := range ops {
		if op.Op == types.FeishuPublishOpSkip && op.Detail != "" {
			preview.Warnings = append(preview.Warnings, op.Detail)
		}
	}
	return preview, payload, targetID, nil
}

// PreviewSync validates credentials/target and returns impact without persistence.
func (s *FeishuPublishService) PreviewSync(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishSyncRequest) (*types.FeishuPublishPreview, error) {
	kb, err := s.authorizeKB(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	secret, err := s.resolveSecret(ctx, tenantID, kbID, req.AppID, req.AppSecret)
	if err != nil {
		return nil, err
	}
	preview, _, _, err := s.buildPreview(ctx, tenantID, kb, req, secret)
	return preview, err
}

// ConfirmSync re-validates, and when impact is unchanged saves config/snapshot/run then enqueues work.
func (s *FeishuPublishService) ConfirmSync(ctx context.Context, tenantID uint64, kbID string, req *types.FeishuPublishSyncRequest) (*types.FeishuPublishConfirmResponse, error) {
	kb, err := s.authorizeKB(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.PreviewDigest) == "" {
		return nil, errors.New("preview_digest is required")
	}
	secret, err := s.resolveSecret(ctx, tenantID, kbID, req.AppID, req.AppSecret)
	if err != nil {
		return nil, err
	}
	preview, payload, targetID, err := s.buildPreview(ctx, tenantID, kb, req, secret)
	if err != nil {
		return nil, err
	}
	if preview.Digest != req.PreviewDigest {
		return &types.FeishuPublishConfirmResponse{
			Accepted: false,
			Preview:  preview,
			Message:  "impact changed; confirm the updated preview",
		}, nil
	}

	active, err := s.repo.GetActiveRun(ctx, tenantID, kbID, targetID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		if active.SnapshotDigest == preview.Digest {
			return &types.FeishuPublishConfirmResponse{Accepted: true, Run: active, Message: "existing run reused"}, nil
		}
		return nil, errFeishuPublishActiveRun
	}

	if existing, _ := s.repo.GetRunByDigest(ctx, tenantID, kbID, targetID, preview.Digest); existing != nil &&
		(existing.Status == types.FeishuPublishRunQueued || existing.Status == types.FeishuPublishRunRunning) {
		return &types.FeishuPublishConfirmResponse{Accepted: true, Run: existing, Message: "existing run reused"}, nil
	}

	cipher, err := utils.EncryptAESGCM(secret, utils.GetAESKey())
	if err != nil {
		return nil, err
	}
	pathJSON, _ := json.Marshal(req.ParentPath)
	payloadJSON, _ := json.Marshal(payload)
	countsJSON, _ := json.Marshal(preview.Counts)
	revision := types.FeishuPublishConfigRevision{
		AppID:           strings.TrimSpace(req.AppID),
		AppSecretCipher: cipher,
		SpaceID:         preview.SpaceID,
		SpaceName:       preview.SpaceName,
		ParentNodeToken: preview.ParentToken,
	}
	revJSON, _ := json.Marshal(revision)

	cfg := &types.FeishuPublishConfig{
		TenantID:            tenantID,
		KnowledgeBaseID:     kbID,
		TargetID:            targetID,
		AppID:               strings.TrimSpace(req.AppID),
		AppSecretCipher:     cipher,
		SpaceID:             preview.SpaceID,
		SpaceName:           preview.SpaceName,
		ParentNodeToken:     preview.ParentToken,
		ParentPath:          types.JSON(pathJSON),
		ConnectionStatus:    types.FeishuPublishConnectionOK,
		LastConnectionError: "",
	}
	if existing, _ := s.repo.GetConfigByKB(ctx, tenantID, kbID); existing != nil {
		cfg.SpaceLocked = existing.SpaceLocked
		cfg.ID = existing.ID
		cfg.LastSuccessAt = existing.LastSuccessAt
	}

	snap := &types.FeishuPublishSnapshot{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		TargetID:        targetID,
		Digest:          preview.Digest,
		Payload:         types.JSON(payloadJSON),
	}
	if err := s.repo.CreateSnapshot(ctx, snap); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertConfig(ctx, cfg); err != nil {
		return nil, err
	}

	run := &types.FeishuPublishRun{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		TargetID:        targetID,
		ConfigID:        cfg.ID,
		SnapshotID:      snap.ID,
		SnapshotDigest:  preview.Digest,
		SpaceID:         preview.SpaceID,
		ParentNodeToken: preview.ParentToken,
		Status:          types.FeishuPublishRunQueued,
		Stage:           "queued",
		ConfigRevision:  types.JSON(revJSON),
		Counts:          types.JSON(countsJSON),
	}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	// Same digest after a terminal run: reset and re-enqueue (spec: admin may sync again).
	if run.Status != types.FeishuPublishRunQueued && run.Status != types.FeishuPublishRunRunning {
		run.ConfigID = cfg.ID
		run.SnapshotID = snap.ID
		run.SpaceID = preview.SpaceID
		run.ParentNodeToken = preview.ParentToken
		run.Status = types.FeishuPublishRunQueued
		run.Stage = "queued"
		run.ConfigRevision = types.JSON(revJSON)
		run.Counts = types.JSON(countsJSON)
		run.ItemResults = types.JSON([]byte("null"))
		run.ErrorCode = ""
		run.ErrorSummary = ""
		run.StartedAt = nil
		run.FinishedAt = nil
		if err := s.repo.UpdateRun(ctx, run); err != nil {
			return nil, err
		}
	}

	payloadTask := &types.FeishuPublishPayload{TenantID: tenantID, RunID: run.ID}
	langfuse.InjectTracing(ctx, payloadTask)
	body, _ := json.Marshal(payloadTask)
	task := asynq.NewTask(types.TypeFeishuPublish, body)
	taskID := "feishu-publish:" + run.ID + ":" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	if _, err := s.taskEnqueuer.Enqueue(
		task,
		asynq.Queue("default"),
		asynq.TaskID(taskID),
		asynq.Timeout(2*time.Hour),
		asynq.MaxRetry(0),
	); err != nil {
		logger.Errorf(ctx, "[FeishuPublish] enqueue failed run=%s: %v", run.ID, err)
		run.Status = types.FeishuPublishRunFailed
		run.Stage = "enqueue_failed"
		run.ErrorCode = "enqueue_failed"
		run.ErrorSummary = "failed to enqueue publish task"
		now := time.Now().UTC()
		run.FinishedAt = &now
		_ = s.repo.UpdateRun(ctx, run)
		return &types.FeishuPublishConfirmResponse{Accepted: false, Run: run, Message: "task enqueue failed"}, nil
	}

	return &types.FeishuPublishConfirmResponse{Accepted: true, Run: run}, nil
}

// Unbind clears credentials and target while retaining mappings/runs.
func (s *FeishuPublishService) Unbind(ctx context.Context, tenantID uint64, kbID string) error {
	if _, err := s.authorizeKB(ctx, tenantID, kbID); err != nil {
		return err
	}
	cfg, err := s.repo.GetConfigByKB(ctx, tenantID, kbID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	active, err := s.repo.GetActiveRun(ctx, tenantID, kbID, cfg.TargetID)
	if err != nil {
		return err
	}
	if active != nil {
		return errFeishuPublishActiveRun
	}
	return s.repo.SoftDeleteConfig(ctx, tenantID, kbID)
}

// GetRun returns a run scoped to tenant and knowledge base.
func (s *FeishuPublishService) GetRun(ctx context.Context, tenantID uint64, kbID, runID string) (*types.FeishuPublishRun, error) {
	if _, err := s.authorizeKB(ctx, tenantID, kbID); err != nil {
		return nil, err
	}
	run, err := s.repo.GetRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.KnowledgeBaseID != kbID {
		return nil, errors.New("run not found")
	}
	return run, nil
}

// refreshMappingsFromRemote overlays live Feishu title/parent onto in-memory
// mappings so the planner can emit rename/move/recreate under WeKnora-authoritative overwrite.
// Failures to fetch a mapped node mark it conflict (planner recreates / replaces).
// Context cancellation stops early without marking remaining nodes as conflict.
func (s *FeishuPublishService) refreshMappingsFromRemote(ctx context.Context, client *feishuconn.Client, mappings []*types.FeishuPublishMapping) {
	if client == nil || len(mappings) == 0 {
		return
	}
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, m := range mappings {
		if m == nil || m.NodeToken == "" {
			continue
		}
		if m.Status != types.FeishuPublishMappingActive && m.Status != types.FeishuPublishMappingConflict {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(m *types.FeishuPublishMapping) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			node, err := client.GetWikiNode(ctx, m.NodeToken)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Warnf(ctx, "[FeishuPublish] remote node unusable kind=%s id=%s: %v", m.SourceKind, m.SourceID, err)
				m.Status = types.FeishuPublishMappingConflict
				return
			}
			if node.SpaceID != "" && m.SpaceID != "" && node.SpaceID != m.SpaceID {
				m.Status = types.FeishuPublishMappingConflict
				return
			}
			if node.Title != "" {
				m.Title = node.Title
			}
			parent := node.ParentNodeToken
			if parent == "" {
				parent = node.ParentNodeID
			}
			m.ParentNodeToken = parent
			if node.ObjToken != "" {
				m.ObjToken = node.ObjToken
			}
		}(m)
	}
	wg.Wait()
}

// loadFeishuSearchableBody returns a searchable text prefix for Feishu pages.
// Large documents that do not fit the full-body bound still contribute a truncated prefix
// (instead of publishing “无可用解析文本”).
func (s *FeishuPublishService) loadFeishuSearchableBody(ctx context.Context, tenantID uint64, knowledgeID string) (string, bool) {
	chunks, fits, err := s.chunkRepo.ListChunksByKnowledgeIDBounded(
		ctx, tenantID, knowledgeID, types.FeishuPublishMaxBodyBlocks, int64(types.FeishuPublishMaxBodyChars*4),
	)
	if err != nil {
		return "", false
	}
	if !fits {
		page := &types.Pagination{Page: 1, PageSize: types.FeishuPublishMaxBodyBlocks}
		chunks, _, err = s.chunkRepo.ListPagedChunksByKnowledgeID(
			ctx, tenantID, knowledgeID, page,
			[]types.ChunkType{types.ChunkTypeText}, "", "", "", "asc", "",
		)
		if err != nil || len(chunks) == 0 {
			return "", false
		}
	}
	var b strings.Builder
	for _, c := range chunks {
		if c == nil || c.Content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.Content)
		if b.Len() >= types.FeishuPublishMaxBodyChars*2 {
			break
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

func (s *FeishuPublishService) listAllTagsForPublish(ctx context.Context, tenantID uint64, kbID string) ([]*types.KnowledgeTag, error) {
	var all []*types.KnowledgeTag
	page := 1
	for {
		batch, total, err := s.tagRepo.ListByKB(ctx, tenantID, kbID, &types.Pagination{Page: page, PageSize: 1000}, "")
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if int64(len(all)) >= total || len(batch) == 0 {
			break
		}
		page++
	}
	return all, nil
}

func sanitizeFeishuErr(err error) error {
	if err == nil {
		return nil
	}
	msg := feishuconn.SanitizeErrorMessage(err.Error())
	return errors.New(msg)
}