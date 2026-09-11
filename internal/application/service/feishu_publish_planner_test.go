package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func findOps(ops []types.FeishuPublishPlanOp, kind, id string) []types.FeishuPublishPlanOp {
	var out []types.FeishuPublishPlanOp
	for _, op := range ops {
		if op.SourceKind == kind && op.SourceID == id {
			out = append(out, op)
		}
	}
	return out
}

func TestPlanFeishuPublishTableDriven(t *testing.T) {
	homeMapping := &types.FeishuPublishMapping{
		SourceKind: types.FeishuPublishSourceKindHome,
		SourceID:   feishuPublishManagedHomeSourceID,
		NodeToken:  "home-node",
		Title:      types.FeishuPublishHomeMarker,
		Status:     types.FeishuPublishMappingActive,
	}
	archiveMapping := &types.FeishuPublishMapping{
		SourceKind: types.FeishuPublishSourceKindArchive,
		SourceID:   feishuPublishArchiveSourceID,
		NodeToken:  "archive-node",
		Title:      types.FeishuPublishArchiveTitle,
		Status:     types.FeishuPublishMappingActive,
	}

	tests := []struct {
		name      string
		snapshot  *types.FeishuPublishSnapshotPayload
		mappings  []*types.FeishuPublishMapping
		wantOp    string
		wantKind  string
		wantID    string
		check     func(t *testing.T, ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts)
	}{
		{
			name: "retire when deleted and mapping active",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:          "k1",
					Eligibility: types.FeishuPublishEligibilityDeleted,
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind: types.FeishuPublishSourceKindKnowledge,
					SourceID:   "k1",
					Title:      "Doc",
					Status:     types.FeishuPublishMappingActive,
					NodeToken:  "n1",
				},
			},
			wantOp:   types.FeishuPublishOpRetire,
			wantKind: types.FeishuPublishSourceKindKnowledge,
			wantID:   "k1",
		},
		{
			name: "temporary ineligible never retires",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:          "k1",
					Title:       "Doc",
					Eligibility: types.FeishuPublishEligibilityTemporarilyIneligible,
					SkipReason:  "knowledge disabled",
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind: types.FeishuPublishSourceKindKnowledge,
					SourceID:   "k1",
					Title:      "Doc",
					Status:     types.FeishuPublishMappingActive,
					SourceHash: "hash",
					NodeToken:  "n1",
					ParentNodeToken: "home-node",
				},
			},
			check: func(t *testing.T, ops []types.FeishuPublishPlanOp, _ types.FeishuPublishCounts) {
				found := findOps(ops, types.FeishuPublishSourceKindKnowledge, "k1")
				require.Len(t, found, 1)
				require.Equal(t, types.FeishuPublishOpSkip, found[0].Op)
				require.Contains(t, found[0].Detail, "warning")
				for _, op := range ops {
					require.NotEqual(t, types.FeishuPublishOpRetire, op.Op)
				}
			},
		},
		{
			name: "hash unchanged skips",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:               "k1",
					Title:            "Doc",
					FileHash:         "same-hash",
					Eligibility:      types.FeishuPublishEligibilityPresent,
					ExpectedParentID: "",
					Length:           10,
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind:      types.FeishuPublishSourceKindKnowledge,
					SourceID:        "k1",
					Title:           "Doc",
					Status:          types.FeishuPublishMappingActive,
					SourceHash:      "same-hash",
					ParentNodeToken: "home-node",
					NodeToken:       "n1",
				},
			},
			wantOp:   types.FeishuPublishOpSkip,
			wantKind: types.FeishuPublishSourceKindKnowledge,
			wantID:   "k1",
		},
		{
			name: "restore archived present eligible",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:          "k1",
					Title:       "Doc",
					FileHash:    "h",
					Length:      42,
					Eligibility: types.FeishuPublishEligibilityPresent,
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind: types.FeishuPublishSourceKindKnowledge,
					SourceID:   "k1",
					Title:      "Doc",
					Status:     types.FeishuPublishMappingArchived,
					SourceHash: "old",
					NodeToken:  "n1",
				},
			},
			check: func(t *testing.T, ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts) {
				found := findOps(ops, types.FeishuPublishSourceKindKnowledge, "k1")
				require.Len(t, found, 1)
				require.Equal(t, types.FeishuPublishOpRestore, found[0].Op)
				require.Equal(t, int64(42), found[0].UploadBytes)
				require.Equal(t, 1, counts.Restore)
				require.Equal(t, int64(42), counts.UploadBytes)
			},
		},
		{
			name: "move when parent changes",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Directories: []types.FeishuPublishDirectorySnap{
					{ID: "d1", Name: "Dir", ExpectedParent: ""},
				},
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:               "k1",
					Title:            "Doc",
					FileHash:         "h",
					Eligibility:      types.FeishuPublishEligibilityPresent,
					ExpectedParentID: "d1",
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind:      types.FeishuPublishSourceKindDirectory,
					SourceID:        "d1",
					Title:           "Dir",
					Status:          types.FeishuPublishMappingActive,
					NodeToken:       "dir-node",
					ParentNodeToken: "home-node",
				},
				{
					SourceKind:      types.FeishuPublishSourceKindKnowledge,
					SourceID:        "k1",
					Title:           "Doc",
					Status:          types.FeishuPublishMappingActive,
					SourceHash:      "h",
					NodeToken:       "n1",
					ParentNodeToken: "home-node", // was under home, now under d1
				},
			},
			wantOp:   types.FeishuPublishOpMove,
			wantKind: types.FeishuPublishSourceKindKnowledge,
			wantID:   "k1",
		},
		{
			name: "conflict mapping overwrites with replace_content",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:          "k1",
					Title:       "Doc",
					FileHash:    "h",
					Eligibility: types.FeishuPublishEligibilityPresent,
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind: types.FeishuPublishSourceKindKnowledge,
					SourceID:   "k1",
					Title:      "Doc",
					Status:     types.FeishuPublishMappingConflict,
					SourceHash: "h",
					NodeToken:  "n1",
				},
			},
			wantOp:   types.FeishuPublishOpReplaceContent,
			wantKind: types.FeishuPublishSourceKindKnowledge,
			wantID:   "k1",
		},
		{
			name: "parent conflict overwrites and does not block children",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Directories: []types.FeishuPublishDirectorySnap{
					{ID: "d1", Name: "Dir", ExpectedParent: ""},
				},
				Knowledge: []types.FeishuPublishKnowledgeSnap{{
					ID:               "k1",
					Title:            "Child",
					FileHash:         "h",
					Length:           9,
					Eligibility:      types.FeishuPublishEligibilityPresent,
					ExpectedParentID: "d1",
				}},
			},
			mappings: []*types.FeishuPublishMapping{
				homeMapping, archiveMapping,
				{
					SourceKind: types.FeishuPublishSourceKindDirectory,
					SourceID:   "d1",
					Title:      "Dir",
					Status:     types.FeishuPublishMappingConflict,
					NodeToken:  "dir-node",
				},
			},
			check: func(t *testing.T, ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts) {
				dirOps := findOps(ops, types.FeishuPublishSourceKindDirectory, "d1")
				require.Len(t, dirOps, 1)
				require.Equal(t, types.FeishuPublishOpCreate, dirOps[0].Op)
				require.Equal(t, "overwrite conflict", dirOps[0].Detail)

				knowOps := findOps(ops, types.FeishuPublishSourceKindKnowledge, "k1")
				require.Len(t, knowOps, 1)
				require.Equal(t, types.FeishuPublishOpCreate, knowOps[0].Op)
				require.Empty(t, knowOps[0].BlockedBy)
				require.Equal(t, int64(9), knowOps[0].UploadBytes)
				require.Zero(t, counts.Blocked)
				require.Zero(t, counts.Conflict)
			},
		},
		{
			name: "same display name different ids create separately",
			snapshot: &types.FeishuPublishSnapshotPayload{
				Knowledge: []types.FeishuPublishKnowledgeSnap{
					{ID: "k1", Title: "Same", FileHash: "a", Length: 1, Eligibility: types.FeishuPublishEligibilityPresent},
					{ID: "k2", Title: "Same", FileHash: "b", Length: 2, Eligibility: types.FeishuPublishEligibilityPresent},
				},
			},
			mappings: []*types.FeishuPublishMapping{homeMapping, archiveMapping},
			check: func(t *testing.T, ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts) {
				require.Len(t, findOps(ops, types.FeishuPublishSourceKindKnowledge, "k1"), 1)
				require.Len(t, findOps(ops, types.FeishuPublishSourceKindKnowledge, "k2"), 1)
				require.Equal(t, types.FeishuPublishOpCreate, findOps(ops, types.FeishuPublishSourceKindKnowledge, "k1")[0].Op)
				require.Equal(t, types.FeishuPublishOpCreate, findOps(ops, types.FeishuPublishSourceKindKnowledge, "k2")[0].Op)
				require.Equal(t, int64(3), counts.UploadBytes)
			},
		},
		{
			name:     "create managed home and archive when missing",
			snapshot: &types.FeishuPublishSnapshotPayload{},
			mappings: nil,
			check: func(t *testing.T, ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts) {
				home := findOps(ops, types.FeishuPublishSourceKindHome, feishuPublishManagedHomeSourceID)
				archive := findOps(ops, types.FeishuPublishSourceKindArchive, feishuPublishArchiveSourceID)
				require.Len(t, home, 1)
				require.Len(t, archive, 1)
				require.Equal(t, types.FeishuPublishOpCreate, home[0].Op)
				require.Equal(t, types.FeishuPublishOpCreate, archive[0].Op)
				require.GreaterOrEqual(t, counts.Create, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, counts := Plan(tt.snapshot, tt.mappings)
			if tt.check != nil {
				tt.check(t, ops, counts)
				return
			}
			found := findOps(ops, tt.wantKind, tt.wantID)
			require.NotEmpty(t, found)
			require.Equal(t, tt.wantOp, found[0].Op)
		})
	}
}
