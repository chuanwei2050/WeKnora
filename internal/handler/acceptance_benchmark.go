package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AcceptanceBenchmarkHandler keeps suite freezing and run snapshots at the
// API boundary. Execution drivers may be added independently without changing
// the immutable dataset contract.
type AcceptanceBenchmarkHandler struct {
	repo interfaces.AcceptanceBenchmarkRepository
}

func NewAcceptanceBenchmarkHandler(repo interfaces.AcceptanceBenchmarkRepository) *AcceptanceBenchmarkHandler {
	return &AcceptanceBenchmarkHandler{repo: repo}
}

func (h *AcceptanceBenchmarkHandler) tenantID(c *gin.Context) (uint64, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	return tenantID, tenantID != 0
}

func (h *AcceptanceBenchmarkHandler) CreateSuite(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	var suite types.AcceptanceSuiteVersion
	if err := c.ShouldBindJSON(&suite); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	suite.ID, suite.TenantID, suite.CreatedAt = uuid.NewString(), tenantID, time.Now().UTC()
	if suite.Kind == "" {
		suite.Kind = types.AcceptanceSuiteRegular
	}
	if suite.Frozen {
		c.Error(errors.NewBadRequestError("new suites must be frozen through the freeze endpoint"))
		return
	}
	if suite.SuiteID == "" || suite.Version == "" || suite.RoutingTaxonomyID == "" || suite.RoutingTaxonomyVersion == "" {
		c.Error(errors.NewBadRequestError("suite_id, version and routing taxonomy identity are required"))
		return
	}
	if err := h.repo.CreateSuiteVersion(c.Request.Context(), &suite); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": suite})
}

func (h *AcceptanceBenchmarkHandler) ListSuites(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	suites, err := h.repo.ListSuiteVersions(c.Request.Context(), tenantID, strings.TrimSpace(c.Query("suite_id")))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": suites})
}

func (h *AcceptanceBenchmarkHandler) GetSuite(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	suite, err := h.repo.GetSuiteVersion(c.Request.Context(), tenantID, c.Param("suite_version_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if suite == nil {
		c.Error(errors.NewNotFoundError("acceptance suite version not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": suite})
}

func (h *AcceptanceBenchmarkHandler) FreezeSuite(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	if !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("acceptance suite freezing requires a tenant administrator"))
		return
	}
	suite, err := h.repo.GetSuiteVersion(c.Request.Context(), tenantID, c.Param("suite_version_id"))
	if err != nil || suite == nil {
		c.Error(errors.NewNotFoundError("acceptance suite version not found"))
		return
	}
	if err := suite.Freeze(time.Now().UTC()); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.FreezeSuiteVersion(c.Request.Context(), tenantID, suite.ID, *suite.FrozenAt); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": suite})
}

func (h *AcceptanceBenchmarkHandler) CreateRun(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	var run types.AcceptanceRun
	if err := c.ShouldBindJSON(&run); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	run.ID, run.TenantID, run.CreatedAt = uuid.NewString(), tenantID, time.Now().UTC()
	suite, err := h.repo.GetSuiteVersion(c.Request.Context(), tenantID, run.SuiteVersionID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if suite == nil || !suite.Frozen {
		c.Error(errors.NewBadRequestError("runs require a frozen suite version"))
		return
	}
	if run.Snapshot.SuiteVersionID == "" {
		run.Snapshot.SuiteVersionID = suite.ID
	}
	if run.Snapshot.SuiteVersionID != suite.ID {
		c.Error(errors.NewBadRequestError("snapshot suite_version_id does not match run"))
		return
	}
	if run.Snapshot.RoutingTaxonomyID == "" {
		run.Snapshot.RoutingTaxonomyID = suite.RoutingTaxonomyID
	}
	if run.Snapshot.RoutingTaxonomyVersion == "" {
		run.Snapshot.RoutingTaxonomyVersion = suite.RoutingTaxonomyVersion
	}
	run.Snapshot.Profile = run.Profile
	if err := run.Snapshot.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if run.Gate == "" {
		run.Gate = types.GatePending
	}
	if err := h.repo.CreateRun(c.Request.Context(), &run); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": run})
}

func (h *AcceptanceBenchmarkHandler) GetRun(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": run})
}

func (h *AcceptanceBenchmarkHandler) ListRuns(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	runs, err := h.repo.ListRuns(c.Request.Context(), tenantID, strings.TrimSpace(c.Query("suite_version_id")))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": runs})
}

func (h *AcceptanceBenchmarkHandler) ListCaseResults(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	results, err := h.repo.ListCaseResults(c.Request.Context(), tenantID, run.ID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": results})
}

func (h *AcceptanceBenchmarkHandler) FinalizeRun(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	records, err := h.repo.ListCaseResults(c.Request.Context(), tenantID, run.ID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	results := make([]types.AcceptanceCaseResult, 0, len(records))
	ttfts := make([]int64, 0, len(records))
	suite, suiteErr := h.repo.GetSuiteVersion(c.Request.Context(), tenantID, run.SuiteVersionID)
	if suiteErr != nil {
		c.Error(errors.NewInternalServerError(suiteErr.Error()))
		return
	}
	caseByID := make(map[string]types.AcceptanceCase)
	if suite != nil {
		for _, item := range suite.Cases {
			caseByID[item.ID] = item
		}
	}
	for _, record := range records {
		item, exists := caseByID[record.CaseID]
		if !exists {
			c.Error(errors.NewBadRequestError("acceptance case is not in the frozen suite"))
			return
		}
		result := types.RecomputeAcceptanceCaseResult(item, record.Payload)
		results = append(results, result)
		if result.Execution != nil {
			if value, valid := result.Execution.Timing.TTFTMillis(); valid {
				ttfts = append(ttfts, value)
			}
		}
	}
	if err := types.FinalizeAcceptanceRun(run, results, ttfts, time.Now().UTC()); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.UpdateRun(c.Request.Context(), tenantID, run); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	report, reportErr := types.BuildAcceptanceReport(*run, results)
	if reportErr != nil {
		c.Error(errors.NewInternalServerError(reportErr.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": report})
}

func (h *AcceptanceBenchmarkHandler) ReviewCaseResult(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	reviewerID := strings.TrimSpace(c.GetString(types.UserIDContextKey.String()))
	if tenantID == 0 || reviewerID == "" || !types.IsBidReviewKnowledgeAdmin(c.Request.Context()) {
		c.Error(errors.NewForbiddenError("acceptance review requires a tenant administrator"))
		return
	}
	var request struct {
		Passed bool `json:"passed"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	runID, caseID := c.Param("run_id"), c.Param("case_id")
	result, err := h.repo.GetCaseResult(c.Request.Context(), tenantID, runID, caseID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if result == nil {
		c.Error(errors.NewNotFoundError("acceptance case result not found"))
		return
	}
	if err := types.ApplyAcceptanceReview(&result.Payload, request.Passed, reviewerID, time.Now().UTC()); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.UpdateCaseResult(c.Request.Context(), tenantID, result); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *AcceptanceBenchmarkHandler) CreateCaseResult(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	// Validate ownership before accepting a result, then keep the payload
	// immutable and tied to the run/case identifiers.
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	suite, err := h.repo.GetSuiteVersion(c.Request.Context(), tenantID, run.SuiteVersionID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	caseID := c.Param("case_id")
	caseFound := false
	if suite != nil {
		for _, item := range suite.Cases {
			if item.ID == caseID {
				caseFound = true
				break
			}
		}
	}
	if !caseFound {
		c.Error(errors.NewNotFoundError("acceptance case not found"))
		return
	}
	var payload types.AcceptanceCaseResult
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	result := &types.AcceptanceCaseResultRecord{ID: uuid.NewString(), RunID: c.Param("run_id"), CaseID: caseID, Payload: payload}
	if err := h.repo.CreateCaseResult(c.Request.Context(), result); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func (h *AcceptanceBenchmarkHandler) ListArtifacts(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	artifacts, err := h.repo.ListArtifacts(c.Request.Context(), tenantID, run.ID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": artifacts})
}

func (h *AcceptanceBenchmarkHandler) ListMaterials(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	artifacts, err := h.repo.ListArtifacts(c.Request.Context(), tenantID, run.ID)
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": types.BuildAcceptanceMaterialChecklist(artifacts)})
}

func (h *AcceptanceBenchmarkHandler) CreateArtifact(c *gin.Context) {
	tenantID, ok := h.tenantID(c)
	if !ok {
		c.Error(errors.NewUnauthorizedError("unauthorized"))
		return
	}
	run, err := h.repo.GetRun(c.Request.Context(), tenantID, c.Param("run_id"))
	if err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if run == nil {
		c.Error(errors.NewNotFoundError("acceptance run not found"))
		return
	}
	var artifact types.AcceptanceArtifact
	if err := c.ShouldBindJSON(&artifact); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	artifact.ID, artifact.RunID = uuid.NewString(), run.ID
	if err := artifact.Validate(); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.repo.CreateArtifact(c.Request.Context(), &artifact); err != nil {
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": artifact})
}
