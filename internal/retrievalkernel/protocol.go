package retrievalkernel

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	MaxQueriesPerRequest = 5
	MaxQueryBytes        = 4096
	MaxTotalQueryBytes   = 8192
)

// Request contains only retrieval concerns shared by pipeline and agent callers.
type Request struct {
	TenantID           uint64
	Queries            []string
	Targets            types.SearchTargets
	CandidateLimit     int
	VectorRecallLimit  int
	KeywordRecallLimit int
}

// NormalizedRequest is the bounded request consumed by retrieval adapters.
type NormalizedRequest struct {
	TenantID           uint64
	Queries            []string
	Targets            types.SearchTargets
	CandidateLimit     int
	VectorRecallLimit  int
	KeywordRecallLimit int
}

// Diagnostics describes retrieval behavior without prescribing answer control flow.
type Diagnostics struct {
	TargetCount        int
	TaskCount          int
	RawCandidates      int
	GovernedCandidates int
	RerankCandidates   int
	RerankOutcome      Outcome
	QueueWait          time.Duration
	Cancellations      int64
}

// Result is the shared retrieval result envelope. Callers retain ownership of
// answer generation, reflection, streaming, and agent-loop decisions.
type Result struct {
	Candidates  []*types.SearchResult
	Diagnostics Diagnostics
}

func NormalizeRequest(request Request) (NormalizedRequest, error) {
	normalized := NormalizedRequest{
		TenantID:           request.TenantID,
		Queries:            append([]string(nil), request.Queries...),
		Targets:            append(types.SearchTargets(nil), request.Targets...),
		CandidateLimit:     BoundCandidateLimit(request.CandidateLimit),
		VectorRecallLimit:  BoundRecallLimit(request.VectorRecallLimit),
		KeywordRecallLimit: BoundRecallLimit(request.KeywordRecallLimit),
	}
	if err := ValidateQueries(normalized.Queries); err != nil {
		return NormalizedRequest{}, err
	}
	if err := ValidateTargetBounds(normalized.Targets, normalized.CandidateLimit); err != nil {
		return NormalizedRequest{}, err
	}
	return normalized, nil
}

func ValidateQueries(queries []string) error {
	if len(queries) == 0 {
		return fmt.Errorf("retrieval query is required")
	}
	if len(queries) > MaxQueriesPerRequest {
		return fmt.Errorf("retrieval query count %d exceeds limit %d", len(queries), MaxQueriesPerRequest)
	}
	totalBytes := 0
	for index, query := range queries {
		queryBytes := len([]byte(query))
		if strings.TrimSpace(query) == "" {
			return fmt.Errorf("retrieval query %d must not be empty", index+1)
		}
		if queryBytes > MaxQueryBytes {
			return fmt.Errorf("retrieval query %d size %d exceeds limit %d bytes", index+1, queryBytes, MaxQueryBytes)
		}
		totalBytes += queryBytes
	}
	if totalBytes > MaxTotalQueryBytes {
		return fmt.Errorf("retrieval queries total size %d exceeds limit %d bytes", totalBytes, MaxTotalQueryBytes)
	}
	return nil
}

func NewDiagnostics(request NormalizedRequest, plan TargetPlan) Diagnostics {
	return Diagnostics{TargetCount: len(request.Targets), TaskCount: len(request.Queries) * plan.TasksPerQuery}
}
