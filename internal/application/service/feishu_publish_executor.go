package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	feishuconn "github.com/Tencent/WeKnora/internal/datasource/connector/feishu"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// errRemoteUnusable means the mapped Feishu node cannot be reused; callers should recreate.
var errRemoteUnusable = errors.New("remote node unusable")

// ProcessPublish is the asynq entrypoint for TypeFeishuPublish.
func (s *FeishuPublishService) ProcessPublishTask(ctx context.Context, task *asynq.Task) error {
	var payload types.FeishuPublishPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("decode feishu publish payload: %w", err)
	}
	return s.ProcessPublish(ctx, payload.TenantID, payload.RunID)
}

// ProcessPublish executes a queued publish run using its frozen snapshot and config revision.
func (s *FeishuPublishService) ProcessPublish(ctx context.Context, tenantID uint64, runID string) error {
	run, err := s.repo.GetRun(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("run not found")
	}
	if !s.IsEnabled() {
		logger.Infof(ctx, "[FeishuPublish] feature disabled; failing run=%s", runID)
		return s.failRun(ctx, run, "feature_disabled", "feishu knowledge sync is disabled")
	}
	if run.Status != types.FeishuPublishRunQueued && run.Status != types.FeishuPublishRunRunning {
		logger.Infof(ctx, "[FeishuPublish] run=%s already terminal status=%s", runID, run.Status)
		return nil
	}

	claimed, err := s.repo.ClaimRun(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if !claimed {
		logger.Infof(ctx, "[FeishuPublish] run=%s not claimed (another worker owns it or status changed)", runID)
		return nil
	}
	run, err = s.repo.GetRun(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("run not found after claim")
	}

	var revision types.FeishuPublishConfigRevision
	if err := json.Unmarshal(run.ConfigRevision, &revision); err != nil {
		return s.failRun(ctx, run, "bad_revision", "invalid config revision")
	}
	secret, err := utils.DecryptAESGCM(revision.AppSecretCipher, utils.GetAESKey())
	if err != nil || secret == "" {
		return s.failRun(ctx, run, "decrypt_failed", "failed to decrypt credentials")
	}

	snap, err := s.repo.GetSnapshot(ctx, tenantID, run.SnapshotID)
	if err != nil || snap == nil {
		return s.failRun(ctx, run, "snapshot_missing", "snapshot not found")
	}
	var payload types.FeishuPublishSnapshotPayload
	if err := json.Unmarshal(snap.Payload, &payload); err != nil {
		return s.failRun(ctx, run, "snapshot_invalid", "invalid snapshot payload")
	}

	client := feishuconn.NewOfficialClient(revision.AppID, secret)
	mappings, err := s.repo.ListMappings(ctx, tenantID, run.KnowledgeBaseID, run.TargetID)
	if err != nil {
		return s.failRun(ctx, run, "mapping_load", "failed to load mappings")
	}
	if err := s.refreshMappingsFromRemote(ctx, client, mappings); err != nil {
		return s.failRun(ctx, run, "mapping_refresh", safeSummary(err))
	}

	exec := &feishuPublishExecutor{
		svc:      s,
		client:   client,
		run:      run,
		payload:  &payload,
		revision: &revision,
		mappings: indexMappings(mappings),
	}

	run.Stage = "ensure_home"
	run.ProgressLabel = ""
	_ = s.repo.UpdateRun(ctx, run)
	if err := exec.ensureManagedHome(ctx); err != nil {
		return s.failRun(ctx, run, "init_write_denied", safeSummary(err))
	}

	ops, _ := PlanFeishuPublish(&payload, mappings)
	results := make([]types.FeishuPublishItemResult, 0, len(ops))
	success, failed := 0, 0

	workTotal := 0
	for _, op := range ops {
		if op.Op != types.FeishuPublishOpSkip {
			workTotal++
		}
	}
	if workTotal == 0 {
		// All skipped: still show a completed unit so the bar does not stick at 0%.
		workTotal = 1
	}

	run.Stage = "executing"
	run.ProgressTotal = workTotal
	run.ProgressDone = 0
	run.ProgressLabel = ""
	_ = s.repo.UpdateRun(ctx, run)

	lastProgressAt := time.Time{}
	persistProgress := func(force bool) {
		if !force && !lastProgressAt.IsZero() && time.Since(lastProgressAt) < 500*time.Millisecond {
			return
		}
		lastProgressAt = time.Now()
		_ = s.repo.UpdateRun(ctx, run)
	}

	workDone := 0
	for _, op := range ops {
		run.ProgressLabel = firstNonEmpty(op.Title, op.SourceKind+"/"+op.SourceID)
		persistProgress(false)

		if op.Op == types.FeishuPublishOpSkip {
			results = append(results, types.FeishuPublishItemResult{
				SourceKind: op.SourceKind, SourceID: op.SourceID, Op: op.Op, Status: "skipped", Summary: op.Detail,
			})
			continue
		}
		if op.Op == types.FeishuPublishOpBlocked || op.Op == types.FeishuPublishOpConflict {
			results = append(results, types.FeishuPublishItemResult{
				SourceKind: op.SourceKind, SourceID: op.SourceID, Op: op.Op, Status: op.Op, Summary: op.Detail,
			})
			if op.Op == types.FeishuPublishOpConflict {
				failed++
			}
			workDone++
			run.ProgressDone = workDone
			persistProgress(true)
			continue
		}
		if err := exec.applyOp(ctx, op); err != nil {
			failed++
			results = append(results, types.FeishuPublishItemResult{
				SourceKind: op.SourceKind, SourceID: op.SourceID, Op: op.Op,
				Status: "failed", ErrorCode: "item_failed", Summary: safeSummary(err),
			})
			logger.Warnf(ctx, "[FeishuPublish] op failed run=%s kind=%s id=%s op=%s: %v", run.ID, op.SourceKind, op.SourceID, op.Op, err)
			workDone++
			run.ProgressDone = workDone
			persistProgress(true)
			continue
		}
		success++
		results = append(results, types.FeishuPublishItemResult{
			SourceKind: op.SourceKind, SourceID: op.SourceID, Op: op.Op, Status: "succeeded",
		})
		workDone++
		run.ProgressDone = workDone
		persistProgress(true)
	}

	finished := time.Now().UTC()
	run.FinishedAt = &finished
	run.Stage = "done"
	run.ProgressDone = workTotal
	run.ProgressTotal = workTotal
	run.ProgressLabel = ""
	counts := aggregateFeishuPublishCounts(ops)
	counts.Failed = failed
	countsJSON, _ := json.Marshal(counts)
	run.Counts = types.JSON(countsJSON)
	resJSON, _ := json.Marshal(results)
	run.ItemResults = types.JSON(resJSON)

	switch {
	case success == 0 && failed > 0:
		run.Status = types.FeishuPublishRunFailed
	case failed > 0:
		run.Status = types.FeishuPublishRunPartial
	default:
		run.Status = types.FeishuPublishRunSucceeded
	}

	if home := exec.mappings[mappingKey{Kind: types.FeishuPublishSourceKindHome, ID: feishuPublishManagedHomeSourceID}]; home != nil && home.NodeToken != "" {
		run.FeishuHomeURL = fmt.Sprintf("https://feishu.cn/wiki/%s", home.NodeToken)
	}

	if err := s.repo.UpdateRun(ctx, run); err != nil {
		return err
	}

	if run.Status == types.FeishuPublishRunSucceeded || run.Status == types.FeishuPublishRunPartial {
		if cfg, _ := s.repo.GetConfigByKB(ctx, tenantID, run.KnowledgeBaseID); cfg != nil {
			cfg.SpaceLocked = true
			cfg.LastSuccessAt = &finished
			cfg.ConnectionStatus = types.FeishuPublishConnectionOK
			_ = s.repo.UpsertConfig(ctx, cfg)
		}
	}
	return nil
}

func (s *FeishuPublishService) failRun(ctx context.Context, run *types.FeishuPublishRun, code, summary string) error {
	now := time.Now().UTC()
	run.Status = types.FeishuPublishRunFailed
	run.Stage = "failed"
	run.ErrorCode = code
	run.ErrorSummary = summary
	run.FinishedAt = &now
	_ = s.repo.UpdateRun(ctx, run)
	return fmt.Errorf("%s: %s", code, summary)
}

func safeSummary(err error) string {
	if err == nil {
		return ""
	}
	return feishuconn.SanitizeErrorMessage(err.Error())
}

type feishuPublishExecutor struct {
	svc      *FeishuPublishService
	client   *feishuconn.Client
	run      *types.FeishuPublishRun
	payload  *types.FeishuPublishSnapshotPayload
	revision *types.FeishuPublishConfigRevision
	mappings map[mappingKey]*types.FeishuPublishMapping
}

func feishuPublishManagedHomeTitle(kbName, knowledgeBaseID string) string {
	if name := strings.TrimSpace(kbName); name != "" {
		return name
	}
	id := knowledgeBaseID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("KnowledgeMesh · %s", id)
}

func (e *feishuPublishExecutor) managedHomeTitle() string {
	kbName := ""
	if e.payload != nil {
		kbName = e.payload.KnowledgeBaseName
	}
	return feishuPublishManagedHomeTitle(kbName, e.run.KnowledgeBaseID)
}

func (e *feishuPublishExecutor) ensureManagedHome(ctx context.Context) error {
	key := mappingKey{Kind: types.FeishuPublishSourceKindHome, ID: feishuPublishManagedHomeSourceID}
	title := e.managedHomeTitle()
	if m := e.mappings[key]; m != nil && m.NodeToken != "" {
		node, err := e.client.GetWikiNode(ctx, m.NodeToken)
		if err == nil && (node.SpaceID == "" || node.SpaceID == e.run.SpaceID) {
			changed := false
			if m.Status != types.FeishuPublishMappingActive || (m.ObjToken != "" && node.ObjToken != "" && m.ObjToken != node.ObjToken) {
				if node.ObjToken != "" {
					m.ObjToken = node.ObjToken
				}
				m.Status = types.FeishuPublishMappingActive
				changed = true
			}
			if m.Title != title {
				if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, m.NodeToken, title); err != nil {
					return err
				}
				m.Title = title
				changed = true
			}
			if changed {
				m.LastSuccessRunID = e.run.ID
				return e.persistMapping(ctx, m)
			}
			return nil
		}
		logger.Warnf(ctx, "[FeishuPublish] managed home unusable; recreating run=%s: %v", e.run.ID, err)
	}

	node, err := e.client.CreateWikiNode(ctx, e.run.SpaceID, e.run.ParentNodeToken, title, "docx")
	if err != nil {
		return err
	}
	return e.persistMapping(ctx, &types.FeishuPublishMapping{
		TenantID:         e.run.TenantID,
		KnowledgeBaseID:  e.run.KnowledgeBaseID,
		TargetID:         e.run.TargetID,
		SourceKind:       types.FeishuPublishSourceKindHome,
		SourceID:         feishuPublishManagedHomeSourceID,
		SpaceID:          e.run.SpaceID,
		NodeToken:        node.NodeToken,
		ObjToken:         node.ObjToken,
		ParentNodeToken:  e.run.ParentNodeToken,
		Title:            title,
		Status:           types.FeishuPublishMappingActive,
		LastSuccessRunID: e.run.ID,
	})
}

func (e *feishuPublishExecutor) homeToken() string {
	if m := e.mappings[mappingKey{Kind: types.FeishuPublishSourceKindHome, ID: feishuPublishManagedHomeSourceID}]; m != nil {
		return m.NodeToken
	}
	return e.run.ParentNodeToken
}

func (e *feishuPublishExecutor) ensureArchive(ctx context.Context) (string, error) {
	key := mappingKey{Kind: types.FeishuPublishSourceKindArchive, ID: feishuPublishArchiveSourceID}
	if m := e.mappings[key]; m != nil && m.NodeToken != "" {
		node, err := e.client.GetWikiNode(ctx, m.NodeToken)
		if err == nil && (node.SpaceID == "" || node.SpaceID == e.run.SpaceID) {
			changed := false
			if m.Status != types.FeishuPublishMappingActive {
				m.Status = types.FeishuPublishMappingActive
				changed = true
			}
			if m.Title != types.FeishuPublishArchiveTitle {
				if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, m.NodeToken, types.FeishuPublishArchiveTitle); err != nil {
					return "", err
				}
				m.Title = types.FeishuPublishArchiveTitle
				changed = true
			}
			if changed {
				m.LastSuccessRunID = e.run.ID
				_ = e.persistMapping(ctx, m)
			}
			return m.NodeToken, nil
		}
		logger.Warnf(ctx, "[FeishuPublish] archive unusable; recreating run=%s: %v", e.run.ID, err)
	}
	node, err := e.client.CreateWikiNode(ctx, e.run.SpaceID, e.homeToken(), types.FeishuPublishArchiveTitle, "docx")
	if err != nil {
		return "", err
	}
	if err := e.persistMapping(ctx, &types.FeishuPublishMapping{
		TenantID:         e.run.TenantID,
		KnowledgeBaseID:  e.run.KnowledgeBaseID,
		TargetID:         e.run.TargetID,
		SourceKind:       types.FeishuPublishSourceKindArchive,
		SourceID:         feishuPublishArchiveSourceID,
		SpaceID:          e.run.SpaceID,
		NodeToken:        node.NodeToken,
		ObjToken:         node.ObjToken,
		ParentNodeToken:  e.homeToken(),
		Title:            types.FeishuPublishArchiveTitle,
		Status:           types.FeishuPublishMappingActive,
		LastSuccessRunID: e.run.ID,
	}); err != nil {
		return "", err
	}
	return node.NodeToken, nil
}

func (e *feishuPublishExecutor) applyOp(ctx context.Context, op types.FeishuPublishPlanOp) error {
	switch op.SourceKind {
	case types.FeishuPublishSourceKindHome, types.FeishuPublishSourceKindArchive:
		switch op.Op {
		case types.FeishuPublishOpCreate, types.FeishuPublishOpRename:
			if op.SourceKind == types.FeishuPublishSourceKindArchive {
				_, err := e.ensureArchive(ctx)
				return err
			}
			return e.ensureManagedHome(ctx)
		default:
			return nil
		}
	case types.FeishuPublishSourceKindVirtualFolder, types.FeishuPublishSourceKindTag:
		return e.applyNavOp(ctx, op)
	case types.FeishuPublishSourceKindDirectory:
		return e.applyDirectoryOp(ctx, op)
	case types.FeishuPublishSourceKindKnowledge:
		return e.applyKnowledgeOp(ctx, op)
	default:
		return fmt.Errorf("unknown source kind %s", op.SourceKind)
	}
}

func (e *feishuPublishExecutor) resolveParentRef(kind, id, legacyDirectoryParent string) string {
	kind, id = ResolveFeishuPublishParent(kind, id, legacyDirectoryParent)
	if kind == types.FeishuPublishSourceKindHome || kind == "" {
		return e.homeToken()
	}
	if m := e.mappings[mappingKey{Kind: kind, ID: id}]; m != nil && m.NodeToken != "" {
		return m.NodeToken
	}
	return e.homeToken()
}

func (e *feishuPublishExecutor) findNavSnap(kind, id string) *types.FeishuPublishNavSnap {
	switch kind {
	case types.FeishuPublishSourceKindVirtualFolder:
		for i := range e.payload.VirtualFolders {
			if e.payload.VirtualFolders[i].ID == id {
				return &e.payload.VirtualFolders[i]
			}
		}
	case types.FeishuPublishSourceKindTag:
		for i := range e.payload.Tags {
			if e.payload.Tags[i].ID == id {
				return &e.payload.Tags[i]
			}
		}
	}
	return nil
}

func (e *feishuPublishExecutor) applyNavOp(ctx context.Context, op types.FeishuPublishPlanOp) error {
	nav := e.findNavSnap(op.SourceKind, op.SourceID)
	key := mappingKey{Kind: op.SourceKind, ID: op.SourceID}
	existing := e.mappings[key]

	switch op.Op {
	case types.FeishuPublishOpCreate, types.FeishuPublishOpRestore:
		if nav == nil {
			return fmt.Errorf("%s snap missing", op.SourceKind)
		}
		parent := e.resolveParentRef(nav.ExpectedParentKind, nav.ExpectedParentID, "")
		var node *feishuconn.WikiNode
		var err error
		if op.Op == types.FeishuPublishOpRestore && existing != nil && existing.NodeToken != "" {
			if err := e.verifyOwned(ctx, existing); err != nil {
				if !errors.Is(err, errRemoteUnusable) {
					return err
				}
			} else {
				node = &feishuconn.WikiNode{NodeToken: existing.NodeToken, ObjToken: existing.ObjToken}
				if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, existing.NodeToken, nav.Name); err != nil {
					return err
				}
				if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, existing.NodeToken, parent); err != nil {
					return err
				}
			}
		}
		if node == nil {
			node, err = e.client.CreateWikiNode(ctx, e.run.SpaceID, parent, nav.Name, "docx")
			if err != nil {
				return err
			}
		}
		return e.persistMapping(ctx, &types.FeishuPublishMapping{
			TenantID: e.run.TenantID, KnowledgeBaseID: e.run.KnowledgeBaseID, TargetID: e.run.TargetID,
			SourceKind: op.SourceKind, SourceID: nav.ID,
			SpaceID: e.run.SpaceID, NodeToken: node.NodeToken, ObjToken: node.ObjToken,
			ParentNodeToken: parent, Title: nav.Name, Status: types.FeishuPublishMappingActive,
			LastSuccessRunID: e.run.ID,
		})
	case types.FeishuPublishOpRename:
		if existing == nil || nav == nil {
			return fmt.Errorf("mapping missing for rename")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyNavOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, existing.NodeToken, nav.Name); err != nil {
			return err
		}
		existing.Title = nav.Name
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpMove:
		if existing == nil || nav == nil {
			return fmt.Errorf("mapping missing for move")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyNavOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		parent := e.resolveParentRef(nav.ExpectedParentKind, nav.ExpectedParentID, "")
		if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, existing.NodeToken, parent); err != nil {
			return err
		}
		existing.ParentNodeToken = parent
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpRetire:
		return e.retireMapped(ctx, existing, op.Title)
	default:
		return nil
	}
}

func (e *feishuPublishExecutor) resolveParentToken(parentSourceID string) string {
	return e.resolveParentRef(types.FeishuPublishSourceKindDirectory, parentSourceID, parentSourceID)
}

func (e *feishuPublishExecutor) applyDirectoryOp(ctx context.Context, op types.FeishuPublishPlanOp) error {
	var dir *types.FeishuPublishDirectorySnap
	for i := range e.payload.Directories {
		if e.payload.Directories[i].ID == op.SourceID {
			dir = &e.payload.Directories[i]
			break
		}
	}
	key := mappingKey{Kind: types.FeishuPublishSourceKindDirectory, ID: op.SourceID}
	existing := e.mappings[key]

	switch op.Op {
	case types.FeishuPublishOpCreate:
		if dir == nil {
			return fmt.Errorf("directory snap missing")
		}
		parent := e.resolveParentRef(dir.ExpectedParentKind, dir.ExpectedParentID, dir.ExpectedParent)
		node, err := e.client.CreateWikiNode(ctx, e.run.SpaceID, parent, dir.Name, "docx")
		if err != nil {
			return err
		}
		return e.persistMapping(ctx, &types.FeishuPublishMapping{
			TenantID: e.run.TenantID, KnowledgeBaseID: e.run.KnowledgeBaseID, TargetID: e.run.TargetID,
			SourceKind: types.FeishuPublishSourceKindDirectory, SourceID: dir.ID,
			SpaceID: e.run.SpaceID, NodeToken: node.NodeToken, ObjToken: node.ObjToken,
			ParentNodeToken: parent, Title: dir.Name, Status: types.FeishuPublishMappingActive,
			LastSuccessRunID: e.run.ID,
		})
	case types.FeishuPublishOpRename:
		if existing == nil || dir == nil {
			return fmt.Errorf("mapping missing for rename")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyDirectoryOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, existing.NodeToken, dir.Name); err != nil {
			return err
		}
		existing.Title = dir.Name
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpMove:
		if existing == nil || dir == nil {
			return fmt.Errorf("mapping missing for move")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyDirectoryOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		parent := e.resolveParentRef(dir.ExpectedParentKind, dir.ExpectedParentID, dir.ExpectedParent)
		if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, existing.NodeToken, parent); err != nil {
			return err
		}
		existing.ParentNodeToken = parent
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpRetire:
		return e.retireMapped(ctx, existing, op.Title)
	default:
		return nil
	}
}

func (e *feishuPublishExecutor) applyKnowledgeOp(ctx context.Context, op types.FeishuPublishPlanOp) error {
	var snap *types.FeishuPublishKnowledgeSnap
	for i := range e.payload.Knowledge {
		if e.payload.Knowledge[i].ID == op.SourceID {
			snap = &e.payload.Knowledge[i]
			break
		}
	}
	key := mappingKey{Kind: types.FeishuPublishSourceKindKnowledge, ID: op.SourceID}
	existing := e.mappings[key]

	switch op.Op {
	case types.FeishuPublishOpCreate, types.FeishuPublishOpRestore:
		if snap == nil || snap.Eligibility != types.FeishuPublishEligibilityPresent {
			return fmt.Errorf("knowledge not eligible")
		}
		parent := e.resolveParentRef(snap.ExpectedParentKind, snap.ExpectedParentID, "")
		var node *feishuconn.WikiNode
		var err error
		if op.Op == types.FeishuPublishOpRestore && existing != nil && existing.NodeToken != "" {
			if err := e.verifyOwned(ctx, existing); err != nil {
				if !errors.Is(err, errRemoteUnusable) {
					return err
				}
				// Fall through to create a fresh node under the expected parent.
			} else {
				node = &feishuconn.WikiNode{NodeToken: existing.NodeToken, ObjToken: existing.ObjToken}
				if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, existing.NodeToken, snap.Title); err != nil {
					return err
				}
				if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, existing.NodeToken, parent); err != nil {
					return err
				}
			}
		}
		if node == nil {
			node, err = e.client.CreateWikiNode(ctx, e.run.SpaceID, parent, snap.Title, "docx")
			if err != nil {
				return err
			}
		}
		fileBlockID, mediaToken, bodyIDs, err := e.writeKnowledgeContent(ctx, node.ObjToken, snap)
		if err != nil {
			return err
		}
		return e.persistMapping(ctx, &types.FeishuPublishMapping{
			TenantID: e.run.TenantID, KnowledgeBaseID: e.run.KnowledgeBaseID, TargetID: e.run.TargetID,
			SourceKind: types.FeishuPublishSourceKindKnowledge, SourceID: snap.ID,
			SpaceID: e.run.SpaceID, NodeToken: node.NodeToken, ObjToken: node.ObjToken,
			ParentNodeToken: parent, Title: snap.Title,
			BodyBlockIDs: mustJSON(bodyIDs), FileBlockID: fileBlockID, MediaToken: mediaToken,
			SourceHash: snap.FileHash, BodyHash: snap.BodyHash, SourceVersion: snap.SourceVersion,
			Status: types.FeishuPublishMappingActive, LastSuccessRunID: e.run.ID,
		})
	case types.FeishuPublishOpReplaceContent:
		if existing == nil || snap == nil {
			return fmt.Errorf("mapping missing for replace")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyKnowledgeOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		fileBlockID, mediaToken, bodyIDs, err := e.replaceKnowledgeContent(ctx, existing, snap)
		if err != nil {
			return err
		}
		existing.FileBlockID = fileBlockID
		existing.MediaToken = mediaToken
		existing.BodyBlockIDs = mustJSON(bodyIDs)
		existing.SourceHash = snap.FileHash
		existing.BodyHash = snap.BodyHash
		existing.SourceVersion = snap.SourceVersion
		existing.Title = snap.Title
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpRename:
		if existing == nil || snap == nil {
			return fmt.Errorf("mapping missing for rename")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyKnowledgeOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, existing.NodeToken, snap.Title); err != nil {
			return err
		}
		existing.Title = snap.Title
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpMove:
		if existing == nil || snap == nil {
			return fmt.Errorf("mapping missing for move")
		}
		if err := e.verifyOwned(ctx, existing); err != nil {
			if errors.Is(err, errRemoteUnusable) {
				return e.applyKnowledgeOp(ctx, types.FeishuPublishPlanOp{
					Op: types.FeishuPublishOpCreate, SourceKind: op.SourceKind, SourceID: op.SourceID, Title: op.Title,
				})
			}
			return err
		}
		parent := e.resolveParentRef(snap.ExpectedParentKind, snap.ExpectedParentID, "")
		if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, existing.NodeToken, parent); err != nil {
			return err
		}
		existing.ParentNodeToken = parent
		existing.Status = types.FeishuPublishMappingActive
		existing.LastSuccessRunID = e.run.ID
		return e.persistMapping(ctx, existing)
	case types.FeishuPublishOpRetire:
		return e.retireMapped(ctx, existing, op.Title)
	default:
		return nil
	}
}

func (e *feishuPublishExecutor) verifyOwned(ctx context.Context, m *types.FeishuPublishMapping) error {
	if m == nil || m.NodeToken == "" {
		return fmt.Errorf("%w: mapping incomplete", errRemoteUnusable)
	}
	node, err := e.client.GetWikiNode(ctx, m.NodeToken)
	if err != nil {
		if feishuconn.IsNotFound(err) {
			return fmt.Errorf("%w: remote node missing: %v", errRemoteUnusable, err)
		}
		return fmt.Errorf("remote node lookup failed: %w", err)
	}
	if node.SpaceID != "" && node.SpaceID != e.run.SpaceID {
		return fmt.Errorf("%w: node outside space", errRemoteUnusable)
	}
	if node.ObjToken != "" && m.ObjToken != node.ObjToken {
		// Same wiki node, refreshed document object — adopt and continue overwrite.
		m.ObjToken = node.ObjToken
	}
	return nil
}

func (e *feishuPublishExecutor) retireMapped(ctx context.Context, m *types.FeishuPublishMapping, title string) error {
	if m == nil {
		return nil
	}
	if err := e.verifyOwned(ctx, m); err != nil {
		if errors.Is(err, errRemoteUnusable) {
			m.Status = types.FeishuPublishMappingArchived
			m.FileBlockID = ""
			m.MediaToken = ""
			m.BodyBlockIDs = nil
			m.LastSuccessRunID = e.run.ID
			return e.persistMapping(ctx, m)
		}
		return err
	}
	archiveToken, err := e.ensureArchive(ctx)
	if err != nil {
		return err
	}
	// Clear managed body/file blocks best-effort before moving.
	if m.ObjToken != "" {
		_ = e.clearManagedBlocks(ctx, m)
		tombstone := fmt.Sprintf("[KnowledgeMesh] 源内容已删除，本页已归档。原标题：%s", title)
		_, _ = e.client.CreateBlockChildren(ctx, m.ObjToken, m.ObjToken, []feishuconn.DocxBlock{feishuconn.NewTextBlock(tombstone)}, 0)
	}
	archivedTitle := "[已归档] " + title
	if _, err := e.client.UpdateWikiNodeTitle(ctx, e.run.SpaceID, m.NodeToken, archivedTitle); err != nil {
		return err
	}
	if _, err := e.client.MoveWikiNode(ctx, e.run.SpaceID, m.NodeToken, archiveToken); err != nil {
		return err
	}
	m.Title = archivedTitle
	m.ParentNodeToken = archiveToken
	m.Status = types.FeishuPublishMappingArchived
	m.FileBlockID = ""
	m.MediaToken = ""
	m.BodyBlockIDs = nil
	m.LastSuccessRunID = e.run.ID
	return e.persistMapping(ctx, m)
}

func (e *feishuPublishExecutor) clearManagedBlocks(ctx context.Context, m *types.FeishuPublishMapping) error {
	if m == nil || m.ObjToken == "" {
		return nil
	}
	var bodyIDs []string
	_ = json.Unmarshal(m.BodyBlockIDs, &bodyIDs)
	wanted := map[string]struct{}{}
	for _, id := range bodyIDs {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	if m.FileBlockID != "" {
		wanted[m.FileBlockID] = struct{}{}
	}
	if len(wanted) == 0 {
		return nil
	}
	blocks, err := e.client.ListDocumentBlocks(ctx, m.ObjToken)
	if err != nil {
		return err
	}
	// Find page root children order and delete matching indices from the end.
	childOrder := []string{}
	for _, b := range blocks {
		if b.BlockType == feishuconn.DocxBlockTypePage {
			childOrder = b.Children
			break
		}
	}
	for i := len(childOrder) - 1; i >= 0; i-- {
		if _, ok := wanted[childOrder[i]]; !ok {
			continue
		}
		_ = e.client.BatchDeleteBlockChildren(ctx, m.ObjToken, m.ObjToken, i, i+1)
	}
	return nil
}

func (e *feishuPublishExecutor) writeKnowledgeContent(ctx context.Context, objToken string, snap *types.FeishuPublishKnowledgeSnap) (fileBlockID, mediaToken string, bodyIDs []string, err error) {
	meta := []string{
		fmt.Sprintf("[KnowledgeMesh托管] 来源标记=%s", types.FeishuPublishHomeMarker),
		fmt.Sprintf("源文件：%s", sanitizeFileName(snap.FileName)),
		fmt.Sprintf("同步时间：%s", time.Now().UTC().Format(time.RFC3339)),
	}
	if snap.BodyText == "" {
		meta = append(meta, "可检索正文：无可用解析文本（附件仍为权威载体）")
	} else if snap.BodyTruncated {
		meta = append(meta, "可检索正文：已截断")
	}
	texts := append([]string{}, meta...)
	if snap.BodyText != "" {
		texts = append(texts, splitBody(snap.BodyText, types.FeishuPublishMaxBodyBlocks)...)
	}
	children := make([]feishuconn.DocxBlock, 0, len(texts))
	for _, t := range texts {
		children = append(children, feishuconn.NewTextBlock(t))
	}
	created, err := e.client.CreateBlockChildren(ctx, objToken, objToken, children, 0)
	if err != nil {
		return "", "", nil, err
	}
	bodyIDs = make([]string, 0, len(created))
	for _, b := range created {
		bodyIDs = append(bodyIDs, b.BlockID)
	}

	data, err := e.openObject(ctx, snap)
	if err != nil {
		return "", "", nil, err
	}
	defer data.Close()
	block, mediaToken, err := e.client.CreateFileBlockAndUploadStream(
		ctx, objToken, objToken, sanitizeFileName(snap.FileName), data.Size, data.Reader, 0,
	)
	if err != nil {
		return "", "", nil, err
	}
	if block != nil {
		fileBlockID = block.BlockID
	}
	return fileBlockID, mediaToken, bodyIDs, nil
}

func (e *feishuPublishExecutor) replaceKnowledgeContent(ctx context.Context, m *types.FeishuPublishMapping, snap *types.FeishuPublishKnowledgeSnap) (fileBlockID, mediaToken string, bodyIDs []string, err error) {
	if m.FileBlockID != "" {
		data, err := e.openObject(ctx, snap)
		if err != nil {
			return "", "", nil, err
		}
		token, err := func() (string, error) {
			defer data.Close()
			return e.client.UploadMediaStream(ctx, m.FileBlockID, sanitizeFileName(snap.FileName), data.Size, data.Reader)
		}()
		if err != nil {
			return "", "", nil, err
		}
		if _, err := e.client.ReplaceFileInBlock(ctx, m.ObjToken, m.FileBlockID, token); err != nil {
			// Fall back to recreate managed content.
			_ = e.clearManagedBlocks(ctx, m)
			return e.writeKnowledgeContent(ctx, m.ObjToken, snap)
		}
		// Refresh body text blocks: clear old body ids then rewrite meta+body, keep file block.
		var oldBody []string
		_ = json.Unmarshal(m.BodyBlockIDs, &oldBody)
		tmp := *m
		tmp.BodyBlockIDs = mustJSON(oldBody)
		tmp.FileBlockID = "" // protect file block from clear
		_ = e.clearManagedBlocks(ctx, &tmp)

		meta := []string{
			fmt.Sprintf("[KnowledgeMesh托管] 来源标记=%s", types.FeishuPublishHomeMarker),
			fmt.Sprintf("源文件：%s", sanitizeFileName(snap.FileName)),
			fmt.Sprintf("同步时间：%s", time.Now().UTC().Format(time.RFC3339)),
		}
		texts := append([]string{}, meta...)
		if snap.BodyText != "" {
			texts = append(texts, splitBody(snap.BodyText, types.FeishuPublishMaxBodyBlocks)...)
		}
		children := make([]feishuconn.DocxBlock, 0, len(texts))
		for _, t := range texts {
			children = append(children, feishuconn.NewTextBlock(t))
		}
		created, err := e.client.CreateBlockChildren(ctx, m.ObjToken, m.ObjToken, children, 0)
		if err != nil {
			return "", "", nil, err
		}
		bodyIDs = make([]string, 0, len(created))
		for _, b := range created {
			bodyIDs = append(bodyIDs, b.BlockID)
		}
		return m.FileBlockID, token, bodyIDs, nil
	}
	_ = e.clearManagedBlocks(ctx, m)
	return e.writeKnowledgeContent(ctx, m.ObjToken, snap)
}

type openedObject struct {
	Reader io.ReadCloser
	Size   int64
}

func (o *openedObject) Close() error {
	if o == nil || o.Reader == nil {
		return nil
	}
	return o.Reader.Close()
}

func (e *feishuPublishExecutor) openObject(ctx context.Context, snap *types.FeishuPublishKnowledgeSnap) (*openedObject, error) {
	if snap.ObjectKey == "" {
		return nil, fmt.Errorf("missing object key")
	}
	if snap.Length <= 0 {
		return nil, fmt.Errorf("missing object length")
	}
	if snap.Length > types.FeishuPublishMaxUploadBytes {
		return nil, fmt.Errorf(
			"file exceeds size limit: %.1f MB > %d MB max",
			float64(snap.Length)/(1024*1024),
			types.FeishuPublishMaxUploadMB,
		)
	}
	// Tenant-scoped path check: object keys typically embed tenant id.
	if !strings.Contains(snap.ObjectKey, fmt.Sprintf("/%d/", e.run.TenantID)) &&
		!strings.HasPrefix(snap.ObjectKey, fmt.Sprintf("%d/", e.run.TenantID)) {
		logger.Warnf(ctx, "[FeishuPublish] object key may not contain tenant id: run=%s", e.run.ID)
	}
	rc, err := e.svc.fileSvc.GetFile(ctx, snap.ObjectKey)
	if err != nil {
		return nil, err
	}
	return &openedObject{Reader: rc, Size: snap.Length}, nil
}

func (e *feishuPublishExecutor) persistMapping(ctx context.Context, m *types.FeishuPublishMapping) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if err := e.svc.repo.UpsertMapping(ctx, m); err != nil {
		return err
	}
	e.mappings[mappingKey{Kind: m.SourceKind, ID: m.SourceID}] = m
	return nil
}

func mustJSON(v any) types.JSON {
	b, _ := json.Marshal(v)
	return types.JSON(b)
}

func sanitizeFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "file.bin"
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == '/' || r == '\\' || r == 0 {
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return "file.bin"
	}
	return out
}

func splitBody(text string, maxBlocks int) []string {
	if maxBlocks <= 0 {
		maxBlocks = 1
	}
	runes := []rune(text)
	const per = 800
	var parts []string
	for len(runes) > 0 && len(parts) < maxBlocks {
		n := per
		if n > len(runes) {
			n = len(runes)
		}
		parts = append(parts, string(runes[:n]))
		runes = runes[n:]
	}
	return parts
}
