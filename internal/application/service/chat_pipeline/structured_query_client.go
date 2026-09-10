package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/structuredquery"
	"github.com/Tencent/WeKnora/internal/types"
)

func (p *PluginDataAnalysis) structuredQueryEnabled() bool {
	return p.config != nil && p.config.StructuredQuery != nil && p.config.StructuredQuery.Enabled && strings.TrimSpace(p.config.StructuredQuery.BaseURL) != ""
}

func (p *PluginDataAnalysis) startStructuredQuery(ctx context.Context, manage *types.ChatManage) <-chan []*types.SearchResult {
	done := make(chan []*types.SearchResult, 1)
	go func() {
		defer close(done)
		kbIDs := manage.SearchTargets.GetAllKnowledgeBaseIDs()
		if len(kbIDs) == 0 {
			kbIDs = append([]string(nil), manage.KnowledgeBaseIDs...)
		}
		results := make([]*types.SearchResult, 0, len(kbIDs))
		var mutex sync.Mutex
		var workers sync.WaitGroup
		maxConcurrency := p.config.StructuredQuery.MaxConcurrency
		if maxConcurrency <= 0 {
			maxConcurrency = 3
		}
		semaphore := make(chan struct{}, maxConcurrency)
		for _, kbID := range kbIDs {
			kbID := kbID
			workers.Add(1)
			go func() {
				defer workers.Done()
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					return
				}
				result, err := p.queryStructuredNamespace(ctx, manage, kbID)
				if err != nil {
					logger.Warnf(ctx, "[StructuredQuery] namespace=%s failed: %v", kbID, err)
					return
				}
				mutex.Lock()
				if result != nil {
					results = append(results, result)
				}
				mutex.Unlock()
			}()
		}
		workers.Wait()
		done <- results
	}()
	return done
}

func (p *PluginDataAnalysis) queryStructuredNamespace(ctx context.Context, manage *types.ChatManage, namespace string) (*types.SearchResult, error) {
	cfg := p.config.StructuredQuery
	payload := structuredquery.Request{Namespace: namespace, Question: manage.Query}
	ids := manage.SearchTargets.KnowledgeIDsForKB(namespace)
	if ids == nil {
		knowledges, err := p.knowledgeService.ListKnowledgeByKnowledgeBaseID(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("list structured datasets: %w", err)
		}
		allowedTags := map[string]struct{}{}
		for _, target := range manage.SearchTargets {
			if target != nil && target.KnowledgeBaseID == namespace {
				for _, tagID := range target.TagIDs {
					allowedTags[tagID] = struct{}{}
				}
			}
		}
		for _, knowledge := range knowledges {
			if knowledge == nil || knowledge.EnableStatus != "enabled" || knowledge.ParseStatus != types.ParseStatusCompleted {
				continue
			}
			if len(allowedTags) > 0 {
				if _, allowed := allowedTags[knowledge.TagID]; !allowed {
					continue
				}
			}
			if datasetID := knowledge.GetMetadata()["structured_dataset_id"]; datasetID != "" {
				payload.DatasetIDs = append(payload.DatasetIDs, datasetID)
			}
		}
	} else {
		for _, id := range ids {
			knowledge, err := p.knowledgeService.GetKnowledgeByID(ctx, id)
			if err != nil || knowledge == nil || knowledge.EnableStatus != "enabled" || knowledge.ParseStatus != types.ParseStatusCompleted {
				continue
			}
			if datasetID := knowledge.GetMetadata()["structured_dataset_id"]; datasetID != "" {
				payload.DatasetIDs = append(payload.DatasetIDs, datasetID)
			}
		}
	}
	if len(payload.DatasetIDs) == 0 {
		return nil, fmt.Errorf("structured datasets unavailable")
	}
	tenantID := manage.SearchTargets.GetTenantIDForKB(namespace)
	if tenantID == 0 {
		tenantID = manage.TenantID
	}
	if tenantID == 0 {
		return nil, fmt.Errorf("structured tenant unavailable")
	}
	started := time.Now()
	response, err := (structuredquery.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Timeout: time.Duration(max(1, cfg.RequestTimeout)) * time.Second}).Query(ctx, tenantID, payload)
	if err != nil {
		return nil, err
	}
	if response.Route == "none" {
		return nil, nil
	}
	if response.Route != "sql" {
		return nil, fmt.Errorf("unexpected structured route %q", response.Route)
	}
	evidence, _ := json.Marshal(map[string]any{
		"instruction": dataAnalysisEvidenceInstruction,
		"columns":     response.Columns,
		"rows":        response.Rows,
		"sources":     response.Sources,
	})
	return &types.SearchResult{
		ID:              "structured_" + namespace,
		Content:         string(evidence),
		Score:           1,
		MatchType:       types.MatchTypeDataAnalysis,
		KnowledgeBaseID: namespace,
		KnowledgeTitle:  "结构化查询结果",
		Metadata: map[string]string{
			"structured_query_total_ms":    fmt.Sprint(response.Timings["total_ms"]),
			"structured_query_model_calls": fmt.Sprint(response.ModelCalls),
			"structured_query_elapsed_ms":  fmt.Sprint(time.Since(started).Milliseconds()),
		},
	}, nil
}
