package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newDirectoryTestRepository(t *testing.T) *knowledgeDirectoryRepository {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = ON").Error)
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES ('kb', 1)").Error)
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.KnowledgeDirectory{}, &types.Knowledge{}, &types.Chunk{}, &types.KnowledgeVersion{}, &types.KnowledgeDirectoryDeleteTask{}, &types.KnowledgeDirectoryDeleteBatch{}, &types.KnowledgeDirectoryDeleteToken{}))
	return &knowledgeDirectoryRepository{db: db}
}

func directorySegments(prefix string, count int) []string {
	segments := make([]string, count)
	for index := range segments {
		segments[index] = fmt.Sprintf("%s-%02d", prefix, index)
	}
	return segments
}

func TestKnowledgeDirectoryEnsurePathEnforcesFinalDepth(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	deepest, err := repo.EnsurePath(t.Context(), 1, "kb", nil, directorySegments("level", types.MaxDirectoryDepth))
	require.NoError(t, err)
	require.NotNil(t, deepest)

	_, err = repo.EnsurePath(t.Context(), 1, "kb", &deepest.ID, []string{"too-deep"})
	require.ErrorIs(t, err, types.ErrInvalidDirectoryPath)
}

func TestKnowledgeDirectoryMoveRejectsSubtreePastFinalDepth(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	target, err := repo.EnsurePath(t.Context(), 1, "kb", nil, directorySegments("target", types.MaxDirectoryDepth-1))
	require.NoError(t, err)
	source, err := repo.EnsurePath(t.Context(), 1, "kb", nil, []string{"source"})
	require.NoError(t, err)
	_, err = repo.EnsurePath(t.Context(), 1, "kb", &source.ID, []string{"child"})
	require.NoError(t, err)

	require.ErrorIs(t, repo.Move(t.Context(), 1, "kb", source.ID, &target.ID), types.ErrInvalidDirectoryPath)
	require.ErrorIs(t, repo.MoveEntries(t.Context(), 1, "kb", []string{source.ID}, nil, &target.ID), types.ErrInvalidDirectoryPath)
}

func TestKnowledgeDirectoryRecursiveDeleteCoordinatesExistingBatchWorker(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	root, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"root"})
	require.NoError(t, err)
	child, err := repo.EnsurePath(ctx, 1, "kb", &root.ID, []string{"child"})
	require.NoError(t, err)
	document := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &child.ID, FileName: "a.txt", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	directories, knowledges, err := repo.ListSubtree(ctx, 1, "kb", root.ID)
	require.NoError(t, err)
	rawToken := "single-use-token"
	tokenHash := sha256.Sum256([]byte(rawToken))
	require.NoError(t, repo.CreateDeleteToken(ctx, &types.KnowledgeDirectoryDeleteToken{TokenHash: hex.EncodeToString(tokenHash[:]), TenantID: 1, KnowledgeBaseID: "kb", RootDirectoryID: root.ID, RequestedBy: "user", SnapshotDigest: directorySnapshotDigest(directories, knowledges), ExpiresAt: time.Now().Add(time.Minute)}))
	task, batches, err := repo.ConfirmDelete(ctx, 1, "kb", root.ID, "user", hex.EncodeToString(tokenHash[:]), time.Now())
	require.NoError(t, err)
	require.Len(t, batches, 1)
	var deletingDocument types.Knowledge
	require.NoError(t, repo.db.First(&deletingDocument, "id = ?", document.ID).Error)
	require.Equal(t, types.ParseStatusDeleting, deletingDocument.ParseStatus)
	require.Equal(t, task.ID, deletingDocument.DeletionTaskID)
	var deletingRoot types.KnowledgeDirectory
	require.NoError(t, repo.db.First(&deletingRoot, "id = ?", root.ID).Error)
	require.Equal(t, types.DirectoryStatusDeleting, deletingRoot.Status)
	require.Error(t, func() error {
		_, _, replayErr := repo.ConfirmDelete(ctx, 1, "kb", root.ID, "user", hex.EncodeToString(tokenHash[:]), time.Now())
		return replayErr
	}())
	payload := &types.KnowledgeListDeletePayload{TenantID: 1, KnowledgeBaseID: "kb", KnowledgeIDs: []string{document.ID}, DirectoryDeleteTaskID: task.ID, DirectoryDeleteBatchID: batches[0].ID}
	require.NoError(t, repo.ValidateDeleteBatch(ctx, payload))
	payload.KnowledgeBaseID = "other"
	require.Error(t, repo.ValidateDeleteBatch(ctx, payload))
	require.NoError(t, repo.db.Where("id = ?", document.ID).Delete(&types.Knowledge{}).Error)
	require.NoError(t, repo.CompleteDeleteBatch(ctx, task.ID, batches[0].ID, nil))
	_, err = repo.Get(ctx, 1, "kb", root.ID)
	require.Error(t, err)
	completed, _, err := repo.GetDeleteTask(ctx, 1, "kb", task.ID)
	require.NoError(t, err)
	require.Equal(t, types.DirectoryDeleteStatusCompleted, completed.Status)
}

func TestKnowledgeDirectoryDeleteConfirmationRejectsInvalidOrChangedTokens(t *testing.T) {
	for name, testCase := range map[string]struct {
		mutate    func(*knowledgeDirectoryRepository, *types.KnowledgeDirectory)
		tokenHash string
		now       time.Time
	}{
		"tampered": {tokenHash: "tampered"},
		"expired":  {now: time.Now().Add(2 * time.Minute)},
		"changed subtree": {mutate: func(repo *knowledgeDirectoryRepository, root *types.KnowledgeDirectory) {
			_, err := repo.EnsurePath(context.Background(), 1, "kb", &root.ID, []string{"new-child"})
			require.NoError(t, err)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			repo := newDirectoryTestRepository(t)
			root, err := repo.EnsurePath(t.Context(), 1, "kb", nil, []string{"root"})
			require.NoError(t, err)
			directories, knowledges, err := repo.ListSubtree(t.Context(), 1, "kb", root.ID)
			require.NoError(t, err)
			rawToken := "valid-token"
			hash := sha256.Sum256([]byte(rawToken))
			storedHash := hex.EncodeToString(hash[:])
			require.NoError(t, repo.CreateDeleteToken(t.Context(), &types.KnowledgeDirectoryDeleteToken{TokenHash: storedHash, TenantID: 1, KnowledgeBaseID: "kb", RootDirectoryID: root.ID, RequestedBy: "user", SnapshotDigest: directorySnapshotDigest(directories, knowledges), ExpiresAt: time.Now().Add(time.Minute)}))
			if testCase.mutate != nil {
				testCase.mutate(repo, root)
			}
			providedHash := storedHash
			if testCase.tokenHash != "" {
				providedHash = testCase.tokenHash
			}
			confirmAt := time.Now()
			if !testCase.now.IsZero() {
				confirmAt = testCase.now
			}
			_, _, err = repo.ConfirmDelete(t.Context(), 1, "kb", root.ID, "user", providedHash, confirmAt)
			require.Error(t, err)
		})
	}
}

func TestKnowledgeDirectoryEnsurePathIsIdempotent(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	first, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"项目", "规范"})
	require.NoError(t, err)
	second, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"项目", "规范"})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	items, total, err := repo.ListChildren(ctx, 1, "kb", nil, 0, 20, "name", "asc", types.KnowledgeVisibilityFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "项目", items[0].Name)
}

func TestKnowledgeDirectoryIsScopedByCategory(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := t.Context()
	categoryA := "category-a"
	categoryB := "category-b"
	directoryA, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"同名目录"}, categoryA)
	require.NoError(t, err)
	directoryB, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"同名目录"}, categoryB)
	require.NoError(t, err)
	require.NotEqual(t, directoryA.ID, directoryB.ID)

	itemsA, totalA, err := repo.ListChildren(ctx, 1, "kb", nil, 0, 20, "name", "asc", types.KnowledgeVisibilityFilter{}, categoryA)
	require.NoError(t, err)
	require.Equal(t, int64(1), totalA)
	require.Equal(t, directoryA.ID, itemsA[0].ID)
	require.Error(t, repo.Create(ctx, &types.KnowledgeDirectory{
		TenantID: 1, KnowledgeBaseID: "kb", TagID: categoryB, ParentID: &directoryA.ID,
		Name: "跨分类子目录", NormalizedName: "跨分类子目录",
	}))

	document := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "kb", TagID: categoryA, DirectoryID: &directoryA.ID, FileName: "a.txt", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	require.Error(t, repo.MoveEntries(ctx, 1, "kb", nil, []string{document.ID}, &directoryB.ID, categoryA))
	stored, err := repo.Get(ctx, 1, "kb", directoryA.ID, categoryA)
	require.NoError(t, err)
	require.Equal(t, categoryA, stored.TagID)
	_, err = repo.Get(ctx, 1, "kb", directoryA.ID, categoryB)
	require.Error(t, err)
}

func TestKnowledgeDirectoryDeleteByTagClearsAssignmentsAndRemovesTree(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	root, err := repo.EnsurePath(t.Context(), 1, "kb", nil, []string{"root"}, "category-a")
	require.NoError(t, err)
	child, err := repo.EnsurePath(t.Context(), 1, "kb", &root.ID, []string{"child"}, "category-a")
	require.NoError(t, err)
	document := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "kb", TagID: "category-a", DirectoryID: &child.ID, FileName: "a.txt"}
	require.NoError(t, repo.db.Create(document).Error)

	require.NoError(t, repo.DeleteByTag(t.Context(), 1, "kb", "category-a"))
	var stored types.Knowledge
	require.NoError(t, repo.db.First(&stored, "id = ?", document.ID).Error)
	require.Nil(t, stored.DirectoryID)
	var count int64
	require.NoError(t, repo.db.Model(&types.KnowledgeDirectory{}).Where("tag_id = ?", "category-a").Count(&count).Error)
	require.Zero(t, count)
}

func TestKnowledgeDirectoryEnsurePathIsConcurrentAndTenantScoped(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	const workers = 12
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			directory, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"Concurrent", "Leaf"})
			if err != nil {
				errs <- err
				return
			}
			ids <- directory.ID
		}()
	}
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var expected string
	for id := range ids {
		if expected == "" {
			expected = id
		}
		require.Equal(t, expected, id)
	}
	var count int64
	require.NoError(t, repo.db.Model(&types.KnowledgeDirectory{}).Where("tenant_id = ? AND knowledge_base_id = ?", 1, "kb").Count(&count).Error)
	require.Equal(t, int64(2), count)

	require.NoError(t, repo.db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES ('other-kb', 2)").Error)
	parent, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"tenant-one"})
	require.NoError(t, err)
	crossTenant := &types.KnowledgeDirectory{TenantID: 2, KnowledgeBaseID: "other-kb", ParentID: &parent.ID, Name: "invalid", NormalizedName: "invalid"}
	require.Error(t, repo.Create(ctx, crossTenant))
}

func TestKnowledgeDirectoryRejectsCycleAndNonEmptyDelete(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	root, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"root"})
	require.NoError(t, err)
	child, err := repo.EnsurePath(ctx, 1, "kb", &root.ID, []string{"child"})
	require.NoError(t, err)
	require.Error(t, repo.Move(ctx, 1, "kb", root.ID, &child.ID))
	require.Error(t, repo.DeleteEmpty(ctx, 1, "kb", root.ID))
	require.NoError(t, repo.DeleteEmpty(ctx, 1, "kb", child.ID))
	require.NoError(t, repo.DeleteEmpty(ctx, 1, "kb", root.ID))
}

func TestKnowledgeDirectorySiblingNameIsUniqueAfterNormalization(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	first := &types.KnowledgeDirectory{TenantID: 1, KnowledgeBaseID: "kb", Name: "Folder", NormalizedName: "folder"}
	second := &types.KnowledgeDirectory{TenantID: 1, KnowledgeBaseID: "kb", Name: "folder", NormalizedName: "folder"}
	require.NoError(t, repo.Create(ctx, first))
	require.Error(t, repo.Create(ctx, second))
}

func TestKnowledgeDirectoryMoveEntriesIsAtomicAndCollapsesDescendants(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	source, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"source"})
	require.NoError(t, err)
	child, err := repo.EnsurePath(ctx, 1, "kb", &source.ID, []string{"child"})
	require.NoError(t, err)
	target, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"target"})
	require.NoError(t, err)
	document := &types.Knowledge{TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &source.ID, FileName: "a.txt", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	require.NoError(t, repo.MoveEntries(ctx, 1, "kb", []string{source.ID, child.ID}, []string{document.ID}, &target.ID))
	movedSource, err := repo.Get(ctx, 1, "kb", source.ID)
	require.NoError(t, err)
	require.Equal(t, target.ID, *movedSource.ParentID)
	movedChild, err := repo.Get(ctx, 1, "kb", child.ID)
	require.NoError(t, err)
	require.Equal(t, source.ID, *movedChild.ParentID)
	var movedDocument types.Knowledge
	require.NoError(t, repo.db.First(&movedDocument, "id = ?", document.ID).Error)
	require.Equal(t, target.ID, *movedDocument.DirectoryID)

	require.Error(t, repo.MoveEntries(ctx, 1, "kb", []string{source.ID}, []string{document.ID}, &child.ID))
	require.NoError(t, repo.db.First(&movedDocument, "id = ?", document.ID).Error)
	require.Equal(t, target.ID, *movedDocument.DirectoryID)
}

func TestKnowledgeDirectoryMoveSubtreesToTagPreservesHierarchyAndDocuments(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := t.Context()
	root, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"source"}, "category-a")
	require.NoError(t, err)
	child, err := repo.EnsurePath(ctx, 1, "kb", &root.ID, []string{"child"}, "category-a")
	require.NoError(t, err)
	document := &types.Knowledge{ID: "cross-category-document", TenantID: 1, KnowledgeBaseID: "kb", TagID: "category-a", DirectoryID: &child.ID, FileName: "a.txt", ParseStatus: types.ParseStatusCompleted}
	chunk := &types.Chunk{ID: "cross-category-chunk", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: document.ID, TagID: "category-a", Content: "content"}
	require.NoError(t, repo.db.Create(document).Error)
	require.NoError(t, repo.db.Create(chunk).Error)

	directDocument := &types.Knowledge{ID: "direct-cross-category-document", TenantID: 1, KnowledgeBaseID: "kb", TagID: "category-a", FileName: "direct.txt", ParseStatus: types.ParseStatusCompleted}
	directChunk := &types.Chunk{ID: "direct-cross-category-chunk", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: directDocument.ID, TagID: "category-a", Content: "direct content"}
	require.NoError(t, repo.db.Create(directDocument).Error)
	require.NoError(t, repo.db.Create(directChunk).Error)

	ids, err := repo.MoveSubtreesToTag(ctx, 1, "kb", "category-a", "category-b", []string{root.ID}, []string{directDocument.ID})
	require.NoError(t, err)
	require.Equal(t, []string{document.ID, directDocument.ID}, ids)
	movedRoot, err := repo.Get(ctx, 1, "kb", root.ID, "category-b")
	require.NoError(t, err)
	require.Nil(t, movedRoot.ParentID)
	movedChild, err := repo.Get(ctx, 1, "kb", child.ID, "category-b")
	require.NoError(t, err)
	require.Equal(t, root.ID, *movedChild.ParentID)
	var movedDocument types.Knowledge
	require.NoError(t, repo.db.First(&movedDocument, "id = ?", document.ID).Error)
	require.Equal(t, "category-b", movedDocument.TagID)
	require.Equal(t, child.ID, *movedDocument.DirectoryID)
	var movedChunk types.Chunk
	require.NoError(t, repo.db.First(&movedChunk, "id = ?", chunk.ID).Error)
	require.Equal(t, "category-b", movedChunk.TagID)
	var movedDirectDocument types.Knowledge
	require.NoError(t, repo.db.First(&movedDirectDocument, "id = ?", directDocument.ID).Error)
	require.Equal(t, "category-b", movedDirectDocument.TagID)
	require.Nil(t, movedDirectDocument.DirectoryID)
}

func TestKnowledgeDirectoryMoveSubtreesToTagRejectsDestinationConflictAtomically(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := t.Context()
	source, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"same-name"}, "category-a")
	require.NoError(t, err)
	_, err = repo.EnsurePath(ctx, 1, "kb", nil, []string{"same-name"}, "category-b")
	require.NoError(t, err)
	document := &types.Knowledge{ID: "atomic-direct-document", TenantID: 1, KnowledgeBaseID: "kb", TagID: "category-a", FileName: "direct.txt", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	_, err = repo.MoveSubtreesToTag(ctx, 1, "kb", "category-a", "category-b", []string{source.ID}, []string{document.ID})
	require.Error(t, err)
	stored, err := repo.Get(ctx, 1, "kb", source.ID, "category-a")
	require.NoError(t, err)
	require.Equal(t, "category-a", stored.TagID)
	var storedDocument types.Knowledge
	require.NoError(t, repo.db.First(&storedDocument, "id = ?", document.ID).Error)
	require.Equal(t, "category-a", storedDocument.TagID)
}

func TestKnowledgeDirectoryMoveSubtreesToTagRejectsDeletingDescendantAtomically(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := t.Context()
	root, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"source"}, "category-a")
	require.NoError(t, err)
	child, err := repo.EnsurePath(ctx, 1, "kb", &root.ID, []string{"child"}, "category-a")
	require.NoError(t, err)
	require.NoError(t, repo.db.Model(&types.KnowledgeDirectory{}).Where("id = ?", child.ID).Update("status", types.DirectoryStatusDeleting).Error)

	_, err = repo.MoveSubtreesToTag(ctx, 1, "kb", "category-a", "category-b", []string{root.ID}, nil)
	require.Error(t, err)
	storedRoot, err := repo.Get(ctx, 1, "kb", root.ID, "category-a")
	require.NoError(t, err)
	require.Equal(t, "category-a", storedRoot.TagID)
}

func TestDirectoryCountsAndPaginationApplyGovernanceBeforeQuery(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	directory, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"visible-count"})
	require.NoError(t, err)
	now := time.Now().UTC()
	version := &types.KnowledgeVersion{ID: "version-active", TenantID: 1, KnowledgeID: "published", Status: types.KnowledgeVersionActive, EffectiveAt: &now}
	require.NoError(t, repo.db.Create(version).Error)
	published := &types.Knowledge{ID: "published", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "published.txt", ParseStatus: types.ParseStatusCompleted, CurrentVersionID: version.ID}
	hidden := &types.Knowledge{ID: "hidden", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "hidden.txt", ParseStatus: types.ParseStatusCompleted, PendingVersionID: "pending"}
	require.NoError(t, repo.db.Create([]*types.Knowledge{published, hidden}).Error)
	visibility := types.KnowledgeVisibilityFilter{Governed: true, Now: now.Add(time.Second)}
	directories, _, err := repo.ListChildren(ctx, 1, "kb", nil, 0, 20, "name", "asc", visibility)
	require.NoError(t, err)
	require.Equal(t, int64(1), directories[0].DocumentCount)
	knowledgeRepo := &knowledgeRepository{db: repo.db}
	documents, total, err := knowledgeRepo.ListPagedKnowledgeByDirectory(ctx, 1, "kb", &directory.ID, 0, 20, "", "", "", "name", "asc", visibility)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "published", documents[0].ID)
}

func TestDirectoryDocumentPaginationUsesStableNaturalNameOrder(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	directory, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"ordered"})
	require.NoError(t, err)
	for _, document := range []*types.Knowledge{
		{ID: "id-10", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "10.txt", ParseStatus: types.ParseStatusCompleted},
		{ID: "id-2b", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "2.txt", ParseStatus: types.ParseStatusCompleted},
		{ID: "id-2a", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "2.txt", ParseStatus: types.ParseStatusCompleted},
	} {
		require.NoError(t, repo.db.Create(document).Error)
	}
	knowledgeRepo := &knowledgeRepository{db: repo.db}
	first, total, err := knowledgeRepo.ListPagedKnowledgeByDirectory(ctx, 1, "kb", &directory.ID, 0, 2, "", "", "", "name", "asc", types.KnowledgeVisibilityFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Equal(t, []string{"id-2a", "id-2b"}, []string{first[0].ID, first[1].ID})
	second, _, err := knowledgeRepo.ListPagedKnowledgeByDirectory(ctx, 1, "kb", &directory.ID, 2, 2, "", "", "", "name", "asc", types.KnowledgeVisibilityFilter{})
	require.NoError(t, err)
	require.Equal(t, []string{"id-10"}, []string{second[0].ID})
}

type namedTestDialector struct {
	gorm.Dialector
	name string
}

func (d namedTestDialector) Name() string { return d.name }

func TestKnowledgeDisplayNameOrderUsesSupportedDialectSyntax(t *testing.T) {
	for name, fragment := range map[string]string{"postgres": "substring", "mysql": "AS UNSIGNED", "sqlite": "GLOB"} {
		db := &gorm.DB{Config: &gorm.Config{Dialector: namedTestDialector{Dialector: sqlite.Open(":memory:"), name: name}}}
		orders := knowledgeDisplayNameOrder(db)
		require.Len(t, orders, 3)
		require.Contains(t, strings.Join(orders, " "), fragment)
	}
}

func TestKnowledgeDirectoryMoveEntriesNoOpPreservesDocument(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	directory, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"same"})
	require.NoError(t, err)
	document := &types.Knowledge{ID: "same-document", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "same.txt", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	require.NoError(t, repo.MoveEntries(ctx, 1, "kb", nil, []string{document.ID}, &directory.ID))
	var stored types.Knowledge
	require.NoError(t, repo.db.First(&stored, "id = ?", document.ID).Error)
	require.Equal(t, directory.ID, *stored.DirectoryID)
}

func TestKnowledgeLifecycleUpdatesPreserveDirectoryAssociation(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	directory, err := repo.EnsurePath(t.Context(), 1, "kb", nil, []string{"lifecycle"})
	require.NoError(t, err)
	document := &types.Knowledge{ID: "lifecycle-document", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "report.docx", FileType: "docx", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, repo.db.Create(document).Error)
	document.ParseStatus = types.ParseStatusPending
	document.Description = ""
	require.NoError(t, (&knowledgeRepository{db: repo.db}).UpdateKnowledge(t.Context(), document))
	var stored types.Knowledge
	require.NoError(t, repo.db.First(&stored, "id = ?", document.ID).Error)
	require.Equal(t, directory.ID, *stored.DirectoryID)
	require.Equal(t, "report.docx", stored.FileName)
	require.Equal(t, "docx", stored.FileType)
}

func TestDirectoryRenameDoesNotChangeDocumentOrChunkIdentity(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	directory, err := repo.EnsurePath(t.Context(), 1, "kb", nil, []string{"before"})
	require.NoError(t, err)
	document := &types.Knowledge{ID: "stable-document", TenantID: 1, KnowledgeBaseID: "kb", DirectoryID: &directory.ID, FileName: "stable.txt", ParseStatus: types.ParseStatusCompleted}
	chunk := &types.Chunk{ID: "stable-chunk", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: document.ID, Content: "stable", IsEnabled: true}
	require.NoError(t, repo.db.Create(document).Error)
	require.NoError(t, repo.db.Create(chunk).Error)
	require.NoError(t, repo.Rename(t.Context(), 1, "kb", directory.ID, "after", "after"))
	var storedDocument types.Knowledge
	var storedChunk types.Chunk
	require.NoError(t, repo.db.First(&storedDocument, "id = ?", document.ID).Error)
	require.NoError(t, repo.db.First(&storedChunk, "id = ?", chunk.ID).Error)
	require.Equal(t, "stable-document", storedDocument.ID)
	require.Equal(t, directory.ID, *storedDocument.DirectoryID)
	require.Equal(t, "stable-chunk", storedChunk.ID)
	require.Equal(t, document.ID, storedChunk.KnowledgeID)
}

func TestKnowledgeDirectoryOppositeConcurrentMovesCannotCreateCycle(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()
	left, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"left"})
	require.NoError(t, err)
	right, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"right"})
	require.NoError(t, err)
	errs := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() { defer group.Done(); errs <- repo.Move(ctx, 1, "kb", left.ID, &right.ID) }()
	go func() { defer group.Done(); errs <- repo.Move(ctx, 1, "kb", right.ID, &left.ID) }()
	group.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	storedLeft, err := repo.Get(ctx, 1, "kb", left.ID)
	require.NoError(t, err)
	storedRight, err := repo.Get(ctx, 1, "kb", right.ID)
	require.NoError(t, err)
	require.False(t, storedLeft.ParentID != nil && storedRight.ParentID != nil)
}

func TestKnowledgeDirectoryCategoryScopesAreIsolated(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	ctx := context.Background()

	directoryA, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"同名目录"}, "tag-a")
	require.NoError(t, err)
	directoryB, err := repo.EnsurePath(ctx, 1, "kb", nil, []string{"同名目录"}, "tag-b")
	require.NoError(t, err)
	require.NotEqual(t, directoryA.ID, directoryB.ID)

	childrenA, totalA, err := repo.ListChildren(ctx, 1, "kb", nil, 0, 10, "name", "asc", types.KnowledgeVisibilityFilter{}, "tag-a")
	require.NoError(t, err)
	require.Equal(t, int64(1), totalA)
	require.Equal(t, directoryA.ID, childrenA[0].ID)

	_, err = repo.EnsurePath(ctx, 1, "kb", &directoryA.ID, []string{"跨分类子目录"}, "tag-b")
	require.Error(t, err)
	require.Error(t, repo.MoveEntries(ctx, 1, "kb", []string{directoryA.ID}, nil, nil, "tag-b"))
}

func TestDirectoryDeletionStorageClaimIsIdempotent(t *testing.T) {
	directoryRepo := newDirectoryTestRepository(t)
	ctx := context.Background()
	tenant := &types.Tenant{ID: 1, StorageUsed: 300}
	require.NoError(t, directoryRepo.db.Create(tenant).Error)
	documents := []*types.Knowledge{
		{ID: "claim-a", TenantID: 1, KnowledgeBaseID: "kb", FileName: "a.txt", ParseStatus: types.ParseStatusDeleting, DeletionTaskID: "task", StorageSize: 100},
		{ID: "claim-b", TenantID: 1, KnowledgeBaseID: "kb", FileName: "b.txt", ParseStatus: types.ParseStatusDeleting, DeletionTaskID: "task", StorageSize: 200},
	}
	require.NoError(t, directoryRepo.db.Create(documents).Error)
	repo := &knowledgeRepository{db: directoryRepo.db}
	claimed, err := repo.ClaimDirectoryDeletionStorage(ctx, 1, "task", []string{"claim-a", "claim-b"})
	require.NoError(t, err)
	require.Equal(t, int64(300), claimed)
	claimed, err = repo.ClaimDirectoryDeletionStorage(ctx, 1, "task", []string{"claim-a", "claim-b"})
	require.NoError(t, err)
	require.Zero(t, claimed)
	require.NoError(t, directoryRepo.db.First(tenant, 1).Error)
	require.Zero(t, tenant.StorageUsed)
}

func TestRetryFailedDeleteBatchesCreatesFreshStableTaskIDs(t *testing.T) {
	repo := newDirectoryTestRepository(t)
	task := &types.KnowledgeDirectoryDeleteTask{ID: "retry-task", TenantID: 1, KnowledgeBaseID: "kb", RootDirectoryID: "root", Status: types.DirectoryDeleteStatusFailed}
	batch := &types.KnowledgeDirectoryDeleteBatch{ID: "retry-batch", DeleteTaskID: task.ID, AsynqTaskID: "old-task-id", Status: types.DirectoryDeleteStatusFailed}
	require.NoError(t, repo.db.Create(task).Error)
	require.NoError(t, repo.db.Create(batch).Error)
	require.NoError(t, repo.RetryFailedDeleteBatches(context.Background(), 1, "kb", task.ID))
	require.NoError(t, repo.db.First(batch, "id = ?", batch.ID).Error)
	require.Equal(t, types.DirectoryDeleteStatusPending, batch.Status)
	require.NotEqual(t, "old-task-id", batch.AsynqTaskID)
	firstRetryID := batch.AsynqTaskID
	require.Error(t, repo.RetryFailedDeleteBatches(context.Background(), 1, "kb", task.ID))
	require.NoError(t, repo.db.First(batch, "id = ?", batch.ID).Error)
	require.Equal(t, firstRetryID, batch.AsynqTaskID)
}
