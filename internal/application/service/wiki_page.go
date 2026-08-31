package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// wikiLinkRegex matches [[wiki-link]] syntax in markdown content
var wikiLinkRegex = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// wikiPageService implements the WikiPageService interface
type wikiPageService struct {
	repo           interfaces.WikiPageRepository
	chunkRepo      interfaces.ChunkRepository
	kbService      interfaces.KnowledgeBaseService
	knowledgeRepo  interfaces.KnowledgeRepository
	governanceRepo interfaces.KnowledgeGovernanceRepository
	redisClient    *redis.Client
}

// NewWikiPageService creates a new wiki page service
func NewWikiPageService(
	repo interfaces.WikiPageRepository,
	chunkRepo interfaces.ChunkRepository,
	kbService interfaces.KnowledgeBaseService,
	knowledgeRepo interfaces.KnowledgeRepository,
	governanceRepo interfaces.KnowledgeGovernanceRepository,
	redisClient *redis.Client,
) interfaces.WikiPageService {
	return &wikiPageService{
		repo:           repo,
		chunkRepo:      chunkRepo,
		kbService:      kbService,
		knowledgeRepo:  knowledgeRepo,
		governanceRepo: governanceRepo,
		redisClient:    redisClient,
	}
}

// CreatePage creates a new wiki page
func (s *wikiPageService) CreatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	if page.ID == "" {
		page.ID = uuid.New().String()
	}
	if page.Slug == "" {
		return nil, errors.New("wiki page slug is required")
	}
	if page.KnowledgeBaseID == "" {
		return nil, errors.New("knowledge_base_id is required")
	}
	if page.Status == "" {
		page.Status = types.WikiPageStatusPublished
	}
	if page.Version == 0 {
		page.Version = 1
	}

	// Parse outbound links from content
	page.OutLinks = s.parseOutLinks(page.Content)

	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now

	if err := s.repo.Create(ctx, page); err != nil {
		return nil, fmt.Errorf("create wiki page: %w", err)
	}

	// Update inbound links on target pages
	s.updateInLinks(ctx, page.KnowledgeBaseID, page.Slug, page.OutLinks)

	return page, nil
}

type knowledgeVersionGuardedWikiPageRepository interface {
	CreateIfKnowledgeVersionsCurrent(ctx context.Context, page *types.WikiPage, expected map[string]string) error
	UpdateIfKnowledgeVersionsCurrent(ctx context.Context, page *types.WikiPage, expected map[string]string, contentChanged bool) error
}

func (s *wikiPageService) CreatePageIfKnowledgeVersionsCurrent(ctx context.Context, page *types.WikiPage, expected map[string]string) (*types.WikiPage, error) {
	guarded, ok := s.repo.(knowledgeVersionGuardedWikiPageRepository)
	if !ok {
		return s.CreatePage(ctx, page)
	}
	if page.ID == "" {
		page.ID = uuid.New().String()
	}
	if page.Slug == "" {
		return nil, errors.New("wiki page slug is required")
	}
	if page.KnowledgeBaseID == "" {
		return nil, errors.New("knowledge_base_id is required")
	}
	if page.Status == "" {
		page.Status = types.WikiPageStatusPublished
	}
	if page.Version == 0 {
		page.Version = 1
	}
	page.OutLinks = s.parseOutLinks(page.Content)
	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now
	if err := guarded.CreateIfKnowledgeVersionsCurrent(ctx, page, expected); err != nil {
		return nil, fmt.Errorf("create wiki page with version guard: %w", err)
	}
	s.updateInLinks(ctx, page.KnowledgeBaseID, page.Slug, page.OutLinks)
	return page, nil
}

// UpdatePage updates an existing wiki page.
//
// Version bump policy: the `version` column is intended to track the user-
// visible content revision, not every row rewrite. We therefore bump it only
// when at least one of the user-facing fields actually changes — title,
// content, summary, page_type, or status. Bookkeeping-only writes (refreshing
// source_refs after re-ingest when the body is identical, rebuilding the index
// page with the same directory, cross-link injection that ends up replacing
// nothing, etc.) still persist through `UpdateMeta` but leave `version`
// untouched so consumers can treat a bump as a real edit signal.
func (s *wikiPageService) UpdatePage(ctx context.Context, page *types.WikiPage) (*types.WikiPage, error) {
	existing, err := s.repo.GetBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	if err != nil {
		return nil, fmt.Errorf("get existing page: %w", err)
	}

	oldOutLinks := existing.OutLinks

	// Snapshot user-visible fields BEFORE mutation so we can decide whether
	// this is a real content change or just bookkeeping.
	contentChanged := existing.Title != page.Title ||
		existing.Content != page.Content ||
		existing.Summary != page.Summary ||
		existing.PageType != page.PageType ||
		existing.Status != page.Status

	existing.Title = page.Title
	existing.Content = page.Content
	existing.Summary = page.Summary
	existing.PageType = page.PageType
	existing.SourceRefs = page.SourceRefs
	existing.ChunkRefs = page.ChunkRefs
	existing.PageMetadata = page.PageMetadata
	existing.Status = page.Status
	existing.UpdatedAt = time.Now()

	// Outbound links are a pure derivative of content, so they only shift
	// when content shifts. Re-parse unconditionally to stay consistent with
	// the stored body.
	existing.OutLinks = s.parseOutLinks(existing.Content)

	if contentChanged {
		if err := s.repo.Update(ctx, existing); err != nil {
			return nil, fmt.Errorf("update wiki page: %w", err)
		}
	} else {
		// No user-visible change — persist bookkeeping fields but preserve
		// the version so downstream consumers can rely on it.
		if err := s.repo.UpdateMeta(ctx, existing); err != nil {
			return nil, fmt.Errorf("update wiki page meta: %w", err)
		}
	}

	// Update inbound links: remove old, add new. If content didn't change,
	// oldOutLinks == existing.OutLinks and these calls are effectively no-ops.
	s.removeInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, oldOutLinks)
	s.updateInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, existing.OutLinks)

	return existing, nil
}

func (s *wikiPageService) UpdatePageIfKnowledgeVersionsCurrent(ctx context.Context, page *types.WikiPage, expected map[string]string) (*types.WikiPage, error) {
	guarded, ok := s.repo.(knowledgeVersionGuardedWikiPageRepository)
	if !ok {
		return s.UpdatePage(ctx, page)
	}
	existing, err := s.repo.GetBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	if err != nil {
		return nil, fmt.Errorf("get existing page: %w", err)
	}
	oldOutLinks := existing.OutLinks
	contentChanged := existing.Title != page.Title ||
		existing.Content != page.Content ||
		existing.Summary != page.Summary ||
		existing.PageType != page.PageType ||
		existing.Status != page.Status
	existing.Title = page.Title
	existing.Content = page.Content
	existing.Summary = page.Summary
	existing.PageType = page.PageType
	existing.SourceRefs = page.SourceRefs
	existing.ChunkRefs = page.ChunkRefs
	existing.PageMetadata = page.PageMetadata
	existing.Status = page.Status
	existing.UpdatedAt = time.Now()
	existing.OutLinks = s.parseOutLinks(existing.Content)
	if err := guarded.UpdateIfKnowledgeVersionsCurrent(ctx, existing, expected, contentChanged); err != nil {
		return nil, fmt.Errorf("update wiki page with version guard: %w", err)
	}
	s.removeInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, oldOutLinks)
	s.updateInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, existing.OutLinks)
	return existing, nil
}

// UpdatePageMeta updates only metadata (status, source_refs) without version bump or link re-parse.
func (s *wikiPageService) UpdatePageMeta(ctx context.Context, page *types.WikiPage) error {
	page.UpdatedAt = time.Now()
	return s.repo.UpdateMeta(ctx, page)
}

// UpdateAutoLinkedContent persists content produced by machine-only link
// decorators (cross-link injection / dead-link cleanup) without bumping
// `version`. Out-links are re-parsed from the new body and bidirectional
// in-link references on target pages are refreshed so link navigation stays
// consistent — only the user-facing revision counter is preserved.
func (s *wikiPageService) UpdateAutoLinkedContent(ctx context.Context, page *types.WikiPage) error {
	existing, err := s.repo.GetBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	if err != nil {
		return fmt.Errorf("get existing page: %w", err)
	}

	oldOutLinks := existing.OutLinks

	existing.Content = page.Content
	existing.OutLinks = s.parseOutLinks(existing.Content)
	existing.UpdatedAt = time.Now()

	if err := s.repo.UpdateAutoLinkedContent(ctx, existing); err != nil {
		return fmt.Errorf("update auto-linked content: %w", err)
	}

	s.removeInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, oldOutLinks)
	s.updateInLinks(ctx, existing.KnowledgeBaseID, existing.Slug, existing.OutLinks)

	return nil
}

// GetPageBySlug retrieves a wiki page by its slug
func (s *wikiPageService) GetPageBySlug(ctx context.Context, kbID string, slug string) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, slug)
	if err != nil {
		return nil, err
	}
	visible, err := s.isWikiPageVisible(ctx, page)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, repository.ErrWikiPageNotFound
	}
	return page, nil
}

// GetPageByID retrieves a wiki page by its ID
func (s *wikiPageService) GetPageByID(ctx context.Context, id string) (*types.WikiPage, error) {
	page, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	visible, err := s.isWikiPageVisible(ctx, page)
	if err != nil {
		return nil, err
	}
	if !visible {
		return nil, repository.ErrWikiPageNotFound
	}
	return page, nil
}

// ListPages lists wiki pages with optional filtering and pagination
func (s *wikiPageService) ListPages(ctx context.Context, req *types.WikiPageListRequest) (*types.WikiPageListResponse, error) {
	pages, total, err := s.repo.List(ctx, req)
	if err != nil {
		return nil, err
	}
	loadedPageCount := len(pages)
	pages, err = s.filterVisibleWikiPages(ctx, pages)
	if err != nil {
		return nil, err
	}
	total -= int64(loadedPageCount - len(pages))
	if total < 0 {
		total = 0
	}

	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	page := req.Page
	if page < 1 {
		page = 1
	}
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	return &types.WikiPageListResponse{
		Pages:      pages,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}, nil
}

// DeletePage soft-deletes a wiki page
func (s *wikiPageService) DeletePage(ctx context.Context, kbID string, slug string) error {
	page, err := s.repo.GetBySlug(ctx, kbID, slug)
	if err != nil {
		return err
	}

	// Remove inbound link references from pages this page links to
	s.removeInLinks(ctx, kbID, slug, page.OutLinks)

	// Delete the page
	if err := s.repo.Delete(ctx, kbID, slug); err != nil {
		return err
	}

	// Delete synced chunk
	s.deleteChunkForPage(ctx, page)

	return nil
}

// GetIndex returns the index page for a knowledge base
func (s *wikiPageService) GetIndex(ctx context.Context, kbID string) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, "index")
	if err != nil {
		if errors.Is(err, repository.ErrWikiPageNotFound) {
			// Create default index page
			return s.createDefaultPage(ctx, kbID, "index", "Index", types.WikiPageTypeIndex,
				"# Wiki Index\n\nThis is the index page. It will be automatically updated as pages are added.\n")
		}
		return nil, err
	}
	return page, nil
}

// GetLog returns the log page for a knowledge base
func (s *wikiPageService) GetLog(ctx context.Context, kbID string) (*types.WikiPage, error) {
	page, err := s.repo.GetBySlug(ctx, kbID, "log")
	if err != nil {
		if errors.Is(err, repository.ErrWikiPageNotFound) {
			return s.createDefaultPage(ctx, kbID, "log", "Log", types.WikiPageTypeLog,
				"# Wiki Operation Log\n\nChronological record of wiki operations.\n")
		}
		return nil, err
	}
	return page, nil
}

// GetGraph returns the link graph data for visualization
func (s *wikiPageService) GetGraph(ctx context.Context, kbID string) (*types.WikiGraphData, error) {
	pages, err := s.ListAllPages(ctx, kbID)
	if err != nil {
		return nil, err
	}

	nodeMap := make(map[string]*types.WikiGraphNode)
	var edges []types.WikiGraphEdge

	// Build nodes
	for _, p := range pages {
		linkCount := len(p.InLinks) + len(p.OutLinks)
		nodeMap[p.Slug] = &types.WikiGraphNode{
			Slug:      p.Slug,
			Title:     p.Title,
			PageType:  p.PageType,
			LinkCount: linkCount,
		}
	}

	// Build edges from outbound links
	for _, p := range pages {
		for _, target := range p.OutLinks {
			if _, exists := nodeMap[target]; exists {
				edges = append(edges, types.WikiGraphEdge{
					Source: p.Slug,
					Target: target,
				})
			}
		}
	}

	nodes := make([]types.WikiGraphNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, *n)
	}

	return &types.WikiGraphData{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// GetStats returns aggregate statistics about the wiki
func (s *wikiPageService) GetStats(ctx context.Context, kbID string) (*types.WikiStats, error) {
	pages, err := s.ListAllPages(ctx, kbID)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int64)
	var total int64
	var orphans int64
	var totalLinks int64
	for _, p := range pages {
		counts[p.PageType]++
		total++
		if len(p.InLinks) == 0 {
			orphans++
		}
		totalLinks += int64(len(p.OutLinks))
	}

	// Get recent updates (last 10)
	listReq := &types.WikiPageListRequest{
		KnowledgeBaseID: kbID,
		Page:            1,
		PageSize:        10,
		SortBy:          "updated_at",
		SortOrder:       "desc",
	}
	recentPages, _, err := s.repo.List(ctx, listReq)
	if err != nil {
		return nil, err
	}
	recentPages, err = s.filterVisibleWikiPages(ctx, recentPages)
	if err != nil {
		return nil, err
	}

	var pendingTasks int64
	var pendingIssues int64
	var isActive bool
	if s.redisClient != nil {
		pendingTasks, _ = s.redisClient.LLen(ctx, "wiki:pending:"+kbID).Result()
		activeFlag, _ := s.redisClient.Exists(ctx, "wiki:active:"+kbID).Result()
		isActive = activeFlag > 0
	}

	issues, _ := s.ListIssues(ctx, kbID, "", "pending")
	pendingIssues = int64(len(issues))

	return &types.WikiStats{
		TotalPages:    total,
		PagesByType:   counts,
		TotalLinks:    totalLinks,
		OrphanCount:   orphans,
		RecentUpdates: recentPages,
		PendingTasks:  pendingTasks,
		PendingIssues: pendingIssues,
		IsActive:      isActive,
	}, nil
}

// RebuildLinks re-parses all pages and rebuilds bidirectional link references
func (s *wikiPageService) RebuildLinks(ctx context.Context, kbID string) error {
	pages, err := s.repo.ListAll(ctx, kbID)
	if err != nil {
		return err
	}

	// Build slug-to-page map
	pageMap := make(map[string]*types.WikiPage)
	for _, p := range pages {
		pageMap[p.Slug] = p
	}

	// Clear all inbound links first
	for _, p := range pages {
		p.InLinks = types.StringArray{}
	}

	// Re-parse outbound links and rebuild inbound links
	for _, p := range pages {
		p.OutLinks = s.parseOutLinks(p.Content)
		for _, target := range p.OutLinks {
			if tp, exists := pageMap[target]; exists {
				tp.InLinks = append(tp.InLinks, p.Slug)
			}
		}
	}

	// Save all pages (link rebuild is metadata-only, no version bump)
	for _, p := range pages {
		p.UpdatedAt = time.Now()
		if err := s.repo.UpdateMeta(ctx, p); err != nil {
			logger.Warnf(ctx, "wiki: failed to update links for page %s: %v", p.Slug, err)
		}
	}

	return nil
}

// ListAllPages retrieves all wiki pages without pagination.
func (s *wikiPageService) ListAllPages(ctx context.Context, kbID string) ([]*types.WikiPage, error) {
	pages, err := s.repo.ListAll(ctx, kbID)
	if err != nil {
		return nil, err
	}
	return s.filterVisibleWikiPages(ctx, pages)
}

// ListPagesBySourceRef exposes the repository's source-ref lookup so higher
// layers (delete flow, retract reconciliation) can re-query the current wiki
// state without depending on a stale caller-captured slug list.
func (s *wikiPageService) ListPagesBySourceRef(ctx context.Context, kbID string, knowledgeID string) ([]*types.WikiPage, error) {
	return s.repo.ListBySourceRef(ctx, kbID, knowledgeID)
}

// SearchPages performs full-text search over wiki pages
func (s *wikiPageService) SearchPages(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error) {
	pages, err := s.repo.Search(ctx, kbID, query, limit)
	if err != nil {
		return nil, err
	}
	return s.filterVisibleWikiPages(ctx, pages)
}

func (s *wikiPageService) filterVisibleWikiPages(ctx context.Context, pages []*types.WikiPage) ([]*types.WikiPage, error) {
	if len(pages) == 0 || s.kbService == nil {
		return pages, nil
	}
	visible := make([]*types.WikiPage, 0, len(pages))
	for _, page := range pages {
		ok, err := s.isWikiPageVisible(ctx, page)
		if err != nil {
			return nil, err
		}
		if ok {
			visible = append(visible, page)
		}
	}
	return visible, nil
}

// isWikiPageVisible prevents a governed source page from remaining readable
// after its source version is pending, expired, or otherwise unavailable.
// Pages without source refs (index/log and user-authored wiki pages) do not
// carry document content and remain visible.
func (s *wikiPageService) isWikiPageVisible(ctx context.Context, page *types.WikiPage) (bool, error) {
	if page == nil || len(page.SourceRefs) == 0 || s.kbService == nil {
		return page != nil, nil
	}
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, page.KnowledgeBaseID)
	if err != nil {
		return false, err
	}
	if kb == nil || !kb.Governance.Enabled {
		return true, nil
	}
	if s.knowledgeRepo == nil || s.governanceRepo == nil {
		return false, nil
	}
	for _, sourceRef := range page.SourceRefs {
		knowledgeID := strings.TrimSpace(strings.SplitN(sourceRef, "|", 2)[0])
		if knowledgeID == "" {
			return false, nil
		}
		knowledge, err := s.knowledgeRepo.GetKnowledgeByIDOnly(ctx, knowledgeID)
		if err != nil {
			return false, err
		}
		if knowledge == nil || knowledge.KnowledgeBaseID != page.KnowledgeBaseID ||
			strings.TrimSpace(knowledge.CurrentVersionID) == "" ||
			strings.TrimSpace(knowledge.PendingVersionID) != "" {
			return false, nil
		}
		version, err := s.governanceRepo.GetVersion(ctx, knowledge.TenantID, knowledge.CurrentVersionID)
		if err != nil {
			return false, err
		}
		if version == nil || !version.IsRetrievable(time.Now().UTC()) {
			return false, nil
		}
	}
	return true, nil
}

// --- Internal helpers ---

// parseOutLinks extracts [[wiki-link]] slugs from markdown content
func (s *wikiPageService) parseOutLinks(content string) types.StringArray {
	matches := wikiLinkRegex.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	var links types.StringArray

	for _, match := range matches {
		if len(match) > 1 {
			slug := strings.TrimSpace(match[1])
			// Handle [[slug|display name]] format — slug is the first part
			if parts := strings.SplitN(slug, "|", 2); len(parts) == 2 {
				slug = strings.TrimSpace(parts[0])
			}
			slug = normalizeSlug(slug)
			if slug != "" && !seen[slug] {
				seen[slug] = true
				links = append(links, slug)
			}
		}
	}
	return links
}

// normalizeSlug normalizes a wiki link slug
func normalizeSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	return slug
}

// updateInLinks adds the source slug to the in_links of target pages
func (s *wikiPageService) updateInLinks(ctx context.Context, kbID string, sourceSlug string, targets types.StringArray) {
	for _, targetSlug := range targets {
		targetPage, err := s.repo.GetBySlug(ctx, kbID, targetSlug)
		if err != nil {
			continue // target page may not exist yet
		}
		if !containsString(targetPage.InLinks, sourceSlug) {
			targetPage.InLinks = append(targetPage.InLinks, sourceSlug)
			targetPage.UpdatedAt = time.Now()
			if err := s.repo.UpdateMeta(ctx, targetPage); err != nil {
				logger.Warnf(ctx, "wiki: failed to update in_links for %s: %v", targetSlug, err)
			}
		}
	}
}

// removeInLinks removes the source slug from the in_links of target pages
func (s *wikiPageService) removeInLinks(ctx context.Context, kbID string, sourceSlug string, targets types.StringArray) {
	for _, targetSlug := range targets {
		targetPage, err := s.repo.GetBySlug(ctx, kbID, targetSlug)
		if err != nil {
			continue
		}
		newInLinks := removeString(targetPage.InLinks, sourceSlug)
		if len(newInLinks) != len(targetPage.InLinks) {
			targetPage.InLinks = newInLinks
			targetPage.UpdatedAt = time.Now()
			if err := s.repo.UpdateMeta(ctx, targetPage); err != nil {
				logger.Warnf(ctx, "wiki: failed to update in_links for %s: %v", targetSlug, err)
			}
		}
	}
}

// deleteChunkForPage removes the synced chunk for a wiki page
func (s *wikiPageService) deleteChunkForPage(ctx context.Context, page *types.WikiPage) {
	chunkID := "wp-" + page.ID
	if err := s.chunkRepo.DeleteChunk(ctx, page.TenantID, chunkID); err != nil {
		logger.Warnf(ctx, "wiki: failed to delete chunk for page %s: %v", page.Slug, err)
	}
}

// createDefaultPage creates a default system page (index, log)
func (s *wikiPageService) createDefaultPage(ctx context.Context, kbID string, slug string, title string, pageType string, content string) (*types.WikiPage, error) {
	// Get KB to get tenant ID
	kb, err := s.kbService.GetKnowledgeBaseByIDOnly(ctx, kbID)
	if err != nil {
		return nil, fmt.Errorf("get knowledge base: %w", err)
	}

	page := &types.WikiPage{
		ID:              uuid.New().String(),
		TenantID:        kb.TenantID,
		KnowledgeBaseID: kbID,
		Slug:            slug,
		Title:           title,
		PageType:        pageType,
		Status:          types.WikiPageStatusPublished,
		Content:         content,
		Summary:         title,
		Version:         1,
	}

	if err := s.repo.Create(ctx, page); err != nil {
		return nil, fmt.Errorf("create default %s page: %w", slug, err)
	}
	return page, nil
}

// containsString checks if a string slice contains a given string
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// removeString removes a string from a slice
func removeString(slice []string, s string) types.StringArray {
	result := make(types.StringArray, 0, len(slice))
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

// CreateIssue logs a new issue for a wiki page
func (s *wikiPageService) CreateIssue(ctx context.Context, issue *types.WikiPageIssue) (*types.WikiPageIssue, error) {
	if issue.ID == "" {
		issue.ID = uuid.New().String()
	}
	if err := s.repo.CreateIssue(ctx, issue); err != nil {
		return nil, fmt.Errorf("create wiki page issue: %w", err)
	}
	return issue, nil
}

// ListIssues retrieves issues for a knowledge base
func (s *wikiPageService) ListIssues(ctx context.Context, kbID string, slug string, status string) ([]*types.WikiPageIssue, error) {
	return s.repo.ListIssues(ctx, kbID, slug, status)
}

// UpdateIssueStatus updates an issue's status
func (s *wikiPageService) UpdateIssueStatus(ctx context.Context, issueID string, status string) error {
	return s.repo.UpdateIssueStatus(ctx, issueID, status)
}

// InjectCrossLinks scans affected pages and injects [[wiki-links]] for mentions
// of other wiki page titles in the content. Pure text replacement, no LLM call.
// Shares the linkifyContent helper with the ingest pipeline so both paths honor
// the same code-block / existing-link / word-boundary rules.
func (s *wikiPageService) InjectCrossLinks(ctx context.Context, kbID string, affectedSlugs []string) {
	allPages, err := s.ListAllPages(ctx, kbID)
	if err != nil || len(allPages) < 2 {
		return
	}

	refs := collectLinkRefs(allPages)
	if len(refs) == 0 {
		return
	}

	affectedSet := make(map[string]bool, len(affectedSlugs))
	for _, slug := range affectedSlugs {
		affectedSet[slug] = true
	}

	var updated int
	for _, p := range allPages {
		if !affectedSet[p.Slug] {
			continue
		}
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}

		newContent, changed := linkifyContent(p.Content, refs, p.Slug)
		if !changed {
			continue
		}
		p.Content = newContent
		if err := s.UpdateAutoLinkedContent(ctx, p); err != nil {
			logger.Warnf(ctx, "wiki: cross-link injection failed for %s: %v", p.Slug, err)
			continue
		}
		updated++
	}

	if updated > 0 {
		logger.Infof(ctx, "wiki: injected cross-links in %d pages", updated)
	}
}

// RebuildIndexPage regenerates the index page directory.
func (s *wikiPageService) RebuildIndexPage(ctx context.Context, kbID string) error {
	indexPage, err := s.GetIndex(ctx, kbID)
	if err != nil {
		return err
	}

	allPages, err := s.ListAllPages(ctx, kbID)
	if err != nil {
		return err
	}

	typeOrder := []string{
		types.WikiPageTypeSummary, types.WikiPageTypeEntity, types.WikiPageTypeConcept,
		types.WikiPageTypeSynthesis, types.WikiPageTypeComparison,
	}
	typeLabels := map[string]string{
		types.WikiPageTypeSummary: "Summary", types.WikiPageTypeEntity: "Entity",
		types.WikiPageTypeConcept: "Concept", types.WikiPageTypeSynthesis: "Synthesis",
		types.WikiPageTypeComparison: "Comparison",
	}

	grouped := make(map[string][]*types.WikiPage)
	totalPages := 0
	for _, p := range allPages {
		if p.PageType == types.WikiPageTypeIndex || p.PageType == types.WikiPageTypeLog {
			continue
		}
		if p.Status == types.WikiPageStatusArchived {
			continue
		}
		grouped[p.PageType] = append(grouped[p.PageType], p)
		totalPages++
	}

	var dir strings.Builder
	for _, pt := range typeOrder {
		pages := grouped[pt]
		if len(pages) == 0 {
			continue
		}
		fmt.Fprintf(&dir, "\n## %s (%d)\n\n", typeLabels[pt], len(pages))
		for _, p := range pages {
			fmt.Fprintf(&dir, "[[%s]] — %s\n", p.Slug, p.Summary)
		}
	}
	for pt, pages := range grouped {
		inOrder := false
		for _, o := range typeOrder {
			if o == pt {
				inOrder = true
				break
			}
		}
		if inOrder || len(pages) == 0 {
			continue
		}
		fmt.Fprintf(&dir, "\n## %s (%d)\n\n", pt, len(pages))
		for _, p := range pages {
			fmt.Fprintf(&dir, "[[%s]] — %s\n", p.Slug, p.Summary)
		}
	}
	if totalPages == 0 {
		dir.WriteString("\n*No wiki pages yet. Upload documents to get started.*\n")
	}

	intro := indexPage.Summary
	if intro == "" {
		intro = "# Wiki Index\n\nThis wiki contains knowledge extracted from uploaded documents.\n"
		indexPage.Summary = intro
	}

	indexPage.Content = intro + "\n" + dir.String()
	_, err = s.UpdatePage(ctx, indexPage)
	return err
}
