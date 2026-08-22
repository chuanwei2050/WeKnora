package interfaces

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type AcceptanceBenchmarkRepository interface {
	CreateSuiteVersion(ctx context.Context, suite *types.AcceptanceSuiteVersion) error
	GetSuiteVersion(ctx context.Context, tenantID uint64, id string) (*types.AcceptanceSuiteVersion, error)
	ListSuiteVersions(ctx context.Context, tenantID uint64, suiteID string) ([]*types.AcceptanceSuiteVersion, error)
	FreezeSuiteVersion(ctx context.Context, tenantID uint64, id string, frozenAt time.Time) error
	CreateRun(ctx context.Context, run *types.AcceptanceRun) error
	UpdateRun(ctx context.Context, tenantID uint64, run *types.AcceptanceRun) error
	GetRun(ctx context.Context, tenantID uint64, id string) (*types.AcceptanceRun, error)
	ListRuns(ctx context.Context, tenantID uint64, suiteVersionID string) ([]*types.AcceptanceRun, error)
	CreateCaseResult(ctx context.Context, result *types.AcceptanceCaseResultRecord) error
	ListCaseResults(ctx context.Context, tenantID uint64, runID string) ([]types.AcceptanceCaseResultRecord, error)
	GetCaseResult(ctx context.Context, tenantID uint64, runID, caseID string) (*types.AcceptanceCaseResultRecord, error)
	UpdateCaseResult(ctx context.Context, tenantID uint64, result *types.AcceptanceCaseResultRecord) error
	CreateArtifact(ctx context.Context, artifact *types.AcceptanceArtifact) error
	ListArtifacts(ctx context.Context, tenantID uint64, runID string) ([]types.AcceptanceArtifact, error)
}
