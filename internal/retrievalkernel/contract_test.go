package retrievalkernel

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestNormalizeRequestProducesSameContractForPipelineAndAgent(t *testing.T) {
	request := Request{
		TenantID:           7,
		Queries:            []string{"shared query"},
		Targets:            types.SearchTargets{{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb"}},
		CandidateLimit:     10_000,
		VectorRecallLimit:  10_000,
		KeywordRecallLimit: 10_000,
	}
	pipelineRequest, pipelineErr := NormalizeRequest(request)
	agentRequest, agentErr := NormalizeRequest(request)
	if pipelineErr != nil || agentErr != nil {
		t.Fatalf("normalize errors: pipeline=%v agent=%v", pipelineErr, agentErr)
	}
	if !reflect.DeepEqual(pipelineRequest, agentRequest) {
		t.Fatalf("normalized contracts differ: pipeline=%+v agent=%+v", pipelineRequest, agentRequest)
	}
	if pipelineRequest.CandidateLimit != MaxCandidatesPerRequest || pipelineRequest.VectorRecallLimit != MaxRecallPerChannel {
		t.Fatalf("request was not bounded: %+v", pipelineRequest)
	}
}

func TestFilterGovernedProducesSameOutcomeForPipelineAndAgentCandidates(t *testing.T) {
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	loadKnowledge := func(_ context.Context, tenantID uint64, _ []string) ([]*types.Knowledge, error) {
		return []*types.Knowledge{
			{ID: "legacy", TenantID: tenantID, KnowledgeBaseID: "kb"},
			{ID: "pending-only", TenantID: tenantID, KnowledgeBaseID: "kb", PendingVersionID: "v1"},
			{ID: "current", TenantID: tenantID, KnowledgeBaseID: "kb", CurrentVersionID: "v2"},
		}, nil
	}
	loadVersion := func(_ context.Context, tenantID uint64, versionID string) (*types.KnowledgeVersion, error) {
		return &types.KnowledgeVersion{ID: versionID, TenantID: tenantID, Status: types.KnowledgeVersionActive}, nil
	}
	newCandidates := func() []GovernanceCandidate {
		return []GovernanceCandidate{
			{AccessTenantID: 7, ExpectedKnowledgeBaseID: "kb", Result: &types.SearchResult{ID: "c1", KnowledgeID: "legacy"}},
			{AccessTenantID: 7, ExpectedKnowledgeBaseID: "kb", Result: &types.SearchResult{ID: "c2", KnowledgeID: "pending-only", KnowledgeVersionID: "v1"}},
			{AccessTenantID: 7, ExpectedKnowledgeBaseID: "kb", Result: &types.SearchResult{ID: "c3", KnowledgeID: "current", KnowledgeVersionID: "v1"}},
			{AccessTenantID: 7, ExpectedKnowledgeBaseID: "kb", Result: &types.SearchResult{ID: "c4", KnowledgeID: "current", KnowledgeVersionID: "v2"}},
		}
	}
	pipeline := FilterGoverned(context.Background(), newCandidates(), loadKnowledge, loadVersion, now)
	agent := FilterGoverned(context.Background(), newCandidates(), loadKnowledge, loadVersion, now)
	if !reflect.DeepEqual(pipeline, agent) {
		t.Fatalf("governance outcomes differ: pipeline=%+v agent=%+v", pipeline, agent)
	}
	want := []bool{true, false, false, true}
	if !reflect.DeepEqual(pipeline.Accepted, want) || pipeline.Rejected["no_active_version"] != 1 || pipeline.Rejected["version_mismatch"] != 1 {
		t.Fatalf("unexpected governance outcome: %+v", pipeline)
	}
}

func TestFilterGovernedRejectsKnowledgeFromAnotherKnowledgeBase(t *testing.T) {
	candidates := []GovernanceCandidate{{
		AccessTenantID: 7, ExpectedKnowledgeBaseID: "shared-kb",
		Result: &types.SearchResult{ID: "chunk", KnowledgeID: "private-document"},
	}}
	decision := FilterGoverned(
		context.Background(),
		candidates,
		func(context.Context, uint64, []string) ([]*types.Knowledge, error) {
			return []*types.Knowledge{{ID: "private-document", TenantID: 99, KnowledgeBaseID: "private-kb"}}, nil
		},
		nil,
		time.Now().UTC(),
	)
	if decision.Accepted[0] || decision.Rejected["knowledge_base_mismatch"] != 1 {
		t.Fatalf("cross-KB candidate was not rejected: %+v", decision)
	}
}

func TestFilterGovernedReportsMissingMetadata(t *testing.T) {
	decision := FilterGoverned(
		context.Background(),
		[]GovernanceCandidate{{
			AccessTenantID: 7, ExpectedKnowledgeBaseID: "shared-kb",
			Result: &types.SearchResult{ID: "chunk", KnowledgeID: "revoked", KnowledgeBaseID: "shared-kb"},
		}},
		func(context.Context, uint64, []string) ([]*types.Knowledge, error) { return nil, nil },
		nil,
		time.Now().UTC(),
	)
	if decision.Accepted[0] || decision.Rejected["metadata_missing"] != 1 {
		t.Fatalf("missing metadata did not fail closed: %+v", decision)
	}
}

func TestFilterGovernedReportsExpiredSeparately(t *testing.T) {
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	expiredAt := now.Add(-time.Minute)
	decision := FilterGoverned(
		context.Background(),
		[]GovernanceCandidate{{
			AccessTenantID: 7, ExpectedKnowledgeBaseID: "kb",
			Result: &types.SearchResult{ID: "chunk", KnowledgeID: "knowledge", KnowledgeVersionID: "version"},
		}},
		func(context.Context, uint64, []string) ([]*types.Knowledge, error) {
			return []*types.Knowledge{{ID: "knowledge", KnowledgeBaseID: "kb", CurrentVersionID: "version"}}, nil
		},
		func(context.Context, uint64, string) (*types.KnowledgeVersion, error) {
			return &types.KnowledgeVersion{ID: "version", Status: types.KnowledgeVersionActive, ExpiresAt: &expiredAt}, nil
		},
		now,
	)
	if decision.Accepted[0] || decision.Rejected["expired"] != 1 || decision.Rejected["not_retrievable"] != 0 {
		t.Fatalf("expired version was not classified separately: %+v", decision)
	}
}

func TestNormalizeRequestRejectsInvalidQueryBounds(t *testing.T) {
	tests := []struct {
		name    string
		queries []string
	}{
		{name: "blank", queries: []string{"  "}},
		{name: "too many", queries: []string{"1", "2", "3", "4", "5", "6"}},
		{name: "single too large", queries: []string{string(make([]byte, MaxQueryBytes+1))}},
		{name: "total too large", queries: []string{
			string(make([]byte, MaxQueryBytes)), string(make([]byte, MaxQueryBytes)), "x",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeRequest(Request{Queries: test.queries}); err == nil {
				t.Fatal("invalid query bounds were accepted")
			}
		})
	}
}

func TestClassifyRerankContractDoesNotChooseCallerControlFlow(t *testing.T) {
	pipelineOutcome := ClassifyRerank(0, nil, true)
	agentOutcome := ClassifyRerank(0, nil, true)
	if pipelineOutcome != OutcomeNoRelevantResult || pipelineOutcome != agentOutcome {
		t.Fatalf("rerank outcomes differ: pipeline=%q agent=%q", pipelineOutcome, agentOutcome)
	}
}
