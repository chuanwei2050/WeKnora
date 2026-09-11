package service

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// Stable source IDs for infrastructure nodes (unique with source_kind under a target).
const (
	feishuPublishManagedHomeSourceID = "managed_home"
	feishuPublishArchiveSourceID     = "archive"
)

type mappingKey struct {
	Kind string
	ID   string
}

// PlanFeishuPublish compares a local snapshot against persisted mappings and returns
// typed remote operations plus aggregated counts. Pure: no network and no DB I/O.
func PlanFeishuPublish(snapshot *types.FeishuPublishSnapshotPayload, mappings []*types.FeishuPublishMapping) (ops []types.FeishuPublishPlanOp, counts types.FeishuPublishCounts) {
	if snapshot == nil {
		snapshot = &types.FeishuPublishSnapshotPayload{}
	}
	byKey := indexMappings(mappings)
	nodeByKey := map[mappingKey]string{}
	for k, m := range byKey {
		nodeByKey[k] = m.NodeToken
	}

	homeTitle := strings.TrimSpace(snapshot.KnowledgeBaseName)
	if homeTitle == "" {
		homeTitle = types.FeishuPublishHomeMarker
	}
	ops = append(ops, planInfrastructure(byKey, homeTitle)...)

	for _, v := range snapshot.VirtualFolders {
		ops = append(ops, planNavNode(types.FeishuPublishSourceKindVirtualFolder, v, byKey, nodeByKey)...)
	}
	for _, tag := range orderNavTopo(snapshot.Tags) {
		ops = append(ops, planNavNode(types.FeishuPublishSourceKindTag, tag, byKey, nodeByKey)...)
	}

	dirByID := map[string]types.FeishuPublishDirectorySnap{}
	for _, d := range orderDirectoriesTopo(snapshot.Directories) {
		dirByID[d.ID] = d
		ops = append(ops, planDirectory(d, byKey, nodeByKey)...)
	}

	knowByID := map[string]types.FeishuPublishKnowledgeSnap{}
	for _, k := range snapshot.Knowledge {
		knowByID[k.ID] = k
		ops = append(ops, planKnowledge(k, byKey, nodeByKey)...)
	}

	seenVirtual := map[string]struct{}{}
	for _, v := range snapshot.VirtualFolders {
		seenVirtual[v.ID] = struct{}{}
	}
	seenTags := map[string]struct{}{}
	for _, tag := range snapshot.Tags {
		seenTags[tag.ID] = struct{}{}
	}
	seenDirs := map[string]struct{}{}
	for id := range dirByID {
		seenDirs[id] = struct{}{}
	}
	seenKnow := map[string]struct{}{}
	for id, k := range knowByID {
		if k.Eligibility != types.FeishuPublishEligibilityDeleted {
			seenKnow[id] = struct{}{}
		}
	}

	for key, m := range byKey {
		switch key.Kind {
		case types.FeishuPublishSourceKindHome, types.FeishuPublishSourceKindArchive:
			continue
		case types.FeishuPublishSourceKindVirtualFolder:
			if _, ok := seenVirtual[key.ID]; ok {
				continue
			}
			ops = append(ops, planMissingMapped(m)...)
		case types.FeishuPublishSourceKindTag:
			if _, ok := seenTags[key.ID]; ok {
				continue
			}
			ops = append(ops, planMissingMapped(m)...)
		case types.FeishuPublishSourceKindDirectory:
			if _, ok := seenDirs[key.ID]; ok {
				continue
			}
			ops = append(ops, planMissingMapped(m)...)
		case types.FeishuPublishSourceKindKnowledge:
			if snap, ok := knowByID[key.ID]; ok {
				if snap.Eligibility == types.FeishuPublishEligibilityDeleted {
					ops = append(ops, planMissingMapped(m)...)
				}
				continue
			}
			ops = append(ops, planMissingMapped(m)...)
		}
	}

	ops = applyParentBlocking(snapshot, ops)
	counts = aggregateFeishuPublishCounts(ops)
	return ops, counts
}

// Plan is an alias matching the design signature.
func Plan(snapshot *types.FeishuPublishSnapshotPayload, mappings []*types.FeishuPublishMapping) ([]types.FeishuPublishPlanOp, types.FeishuPublishCounts) {
	return PlanFeishuPublish(snapshot, mappings)
}

func indexMappings(mappings []*types.FeishuPublishMapping) map[mappingKey]*types.FeishuPublishMapping {
	out := make(map[mappingKey]*types.FeishuPublishMapping, len(mappings))
	for _, m := range mappings {
		if m == nil {
			continue
		}
		out[mappingKey{Kind: m.SourceKind, ID: m.SourceID}] = m
	}
	return out
}

func planInfrastructure(byKey map[mappingKey]*types.FeishuPublishMapping, homeTitle string) []types.FeishuPublishPlanOp {
	var ops []types.FeishuPublishPlanOp
	homeKey := mappingKey{Kind: types.FeishuPublishSourceKindHome, ID: feishuPublishManagedHomeSourceID}
	if m := byKey[homeKey]; m == nil {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindHome,
			SourceID:   feishuPublishManagedHomeSourceID,
			Title:      homeTitle,
		})
	} else if m.Status == types.FeishuPublishMappingConflict {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindHome,
			SourceID:   feishuPublishManagedHomeSourceID,
			Title:      homeTitle,
			Detail:     "overwrite conflict",
		})
	} else {
		if m.Title != homeTitle {
			ops = append(ops, types.FeishuPublishPlanOp{
				Op:         types.FeishuPublishOpRename,
				SourceKind: types.FeishuPublishSourceKindHome,
				SourceID:   feishuPublishManagedHomeSourceID,
				Title:      homeTitle,
				Detail:     fmt.Sprintf("title %q → %q", m.Title, homeTitle),
			})
		} else {
			ops = append(ops, types.FeishuPublishPlanOp{
				Op:         types.FeishuPublishOpSkip,
				SourceKind: types.FeishuPublishSourceKindHome,
				SourceID:   feishuPublishManagedHomeSourceID,
				Title:      firstNonEmpty(m.Title, homeTitle),
			})
		}
	}

	archiveKey := mappingKey{Kind: types.FeishuPublishSourceKindArchive, ID: feishuPublishArchiveSourceID}
	if m := byKey[archiveKey]; m == nil {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindArchive,
			SourceID:   feishuPublishArchiveSourceID,
			Title:      types.FeishuPublishArchiveTitle,
		})
	} else if m.Status == types.FeishuPublishMappingConflict {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindArchive,
			SourceID:   feishuPublishArchiveSourceID,
			Title:      types.FeishuPublishArchiveTitle,
			Detail:     "overwrite conflict",
		})
	} else if m.Title != types.FeishuPublishArchiveTitle {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpRename,
			SourceKind: types.FeishuPublishSourceKindArchive,
			SourceID:   feishuPublishArchiveSourceID,
			Title:      types.FeishuPublishArchiveTitle,
			Detail:     fmt.Sprintf("title %q → %q", m.Title, types.FeishuPublishArchiveTitle),
		})
	} else {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: types.FeishuPublishSourceKindArchive,
			SourceID:   feishuPublishArchiveSourceID,
			Title:      firstNonEmpty(m.Title, types.FeishuPublishArchiveTitle),
		})
	}
	return ops
}

func planNavNode(kind string, n types.FeishuPublishNavSnap, byKey map[mappingKey]*types.FeishuPublishMapping, nodeByKey map[mappingKey]string) []types.FeishuPublishPlanOp {
	key := mappingKey{Kind: kind, ID: n.ID}
	m := byKey[key]
	if m == nil {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
		}}
	}
	if m.Status == types.FeishuPublishMappingConflict {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
			Detail:     "overwrite conflict",
		}}
	}
	if m.Status == types.FeishuPublishMappingArchived {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpRestore,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
		}}
	}

	var ops []types.FeishuPublishPlanOp
	if m.Title != n.Name {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpRename,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
			Detail:     fmt.Sprintf("title %q → %q", m.Title, n.Name),
		})
	}
	expectedParent := expectedParentNodeToken(n.ExpectedParentKind, n.ExpectedParentID, "", nodeByKey)
	if m.ParentNodeToken != expectedParent {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpMove,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
			Detail:     fmt.Sprintf("parent %q → %q", m.ParentNodeToken, expectedParent),
		})
	}
	if len(ops) == 0 {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: kind,
			SourceID:   n.ID,
			Title:      n.Name,
		})
	}
	return ops
}

func planDirectory(d types.FeishuPublishDirectorySnap, byKey map[mappingKey]*types.FeishuPublishMapping, nodeByKey map[mappingKey]string) []types.FeishuPublishPlanOp {
	key := mappingKey{Kind: types.FeishuPublishSourceKindDirectory, ID: d.ID}
	m := byKey[key]
	if m == nil {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
		}}
	}
	if m.Status == types.FeishuPublishMappingConflict {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpCreate,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
			Detail:     "overwrite conflict",
		}}
	}
	if m.Status == types.FeishuPublishMappingArchived {
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpRestore,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
		}}
	}

	var ops []types.FeishuPublishPlanOp
	if m.Title != d.Name {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpRename,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
			Detail:     fmt.Sprintf("title %q → %q", m.Title, d.Name),
		})
	}
	parentKind, parentID := ResolveFeishuPublishParent(d.ExpectedParentKind, d.ExpectedParentID, d.ExpectedParent)
	expectedParent := expectedParentNodeToken(parentKind, parentID, "", nodeByKey)
	if m.ParentNodeToken != expectedParent {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpMove,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
			Detail:     fmt.Sprintf("parent %q → %q", m.ParentNodeToken, expectedParent),
		})
	}
	if len(ops) == 0 {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: types.FeishuPublishSourceKindDirectory,
			SourceID:   d.ID,
			Title:      d.Name,
		})
	}
	return ops
}

func planKnowledge(k types.FeishuPublishKnowledgeSnap, byKey map[mappingKey]*types.FeishuPublishMapping, nodeByKey map[mappingKey]string) []types.FeishuPublishPlanOp {
	if k.Eligibility == types.FeishuPublishEligibilityDeleted {
		return nil
	}

	key := mappingKey{Kind: types.FeishuPublishSourceKindKnowledge, ID: k.ID}
	m := byKey[key]

	if k.Eligibility == types.FeishuPublishEligibilityTemporarilyIneligible {
		title := k.Title
		if title == "" {
			title = k.FileName
		}
		detail := k.SkipReason
		if detail == "" {
			detail = "temporarily_ineligible"
		}
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: types.FeishuPublishSourceKindKnowledge,
			SourceID:   k.ID,
			Title:      title,
			Detail:     "warning: " + detail,
		}}
	}

	title := firstNonEmpty(k.Title, k.FileName)
	parentKind, parentID := ResolveFeishuPublishParent(k.ExpectedParentKind, k.ExpectedParentID, "")
	if m == nil {
		return []types.FeishuPublishPlanOp{{
			Op:          types.FeishuPublishOpCreate,
			SourceKind:  types.FeishuPublishSourceKindKnowledge,
			SourceID:    k.ID,
			Title:       title,
			UploadBytes: k.Length,
		}}
	}
	if m.Status == types.FeishuPublishMappingConflict {
		ops := []types.FeishuPublishPlanOp{{
			Op:          types.FeishuPublishOpReplaceContent,
			SourceKind:  types.FeishuPublishSourceKindKnowledge,
			SourceID:    k.ID,
			Title:       title,
			UploadBytes: k.Length,
			Detail:      "overwrite conflict",
		}}
		mappedTitle := m.Title
		if mappedTitle != title {
			ops = append(ops, types.FeishuPublishPlanOp{
				Op:         types.FeishuPublishOpRename,
				SourceKind: types.FeishuPublishSourceKindKnowledge,
				SourceID:   k.ID,
				Title:      title,
				Detail:     fmt.Sprintf("title %q → %q", mappedTitle, title),
			})
		}
		expectedParent := expectedParentNodeToken(parentKind, parentID, "", nodeByKey)
		if m.ParentNodeToken != expectedParent {
			ops = append(ops, types.FeishuPublishPlanOp{
				Op:         types.FeishuPublishOpMove,
				SourceKind: types.FeishuPublishSourceKindKnowledge,
				SourceID:   k.ID,
				Title:      title,
				Detail:     fmt.Sprintf("parent %q → %q", m.ParentNodeToken, expectedParent),
			})
		}
		return ops
	}
	if m.Status == types.FeishuPublishMappingArchived {
		return []types.FeishuPublishPlanOp{{
			Op:          types.FeishuPublishOpRestore,
			SourceKind:  types.FeishuPublishSourceKindKnowledge,
			SourceID:    k.ID,
			Title:       title,
			UploadBytes: k.Length,
		}}
	}

	var ops []types.FeishuPublishPlanOp
	if m.SourceHash != k.FileHash || (k.BodyHash != "" && m.BodyHash != k.BodyHash) {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:          types.FeishuPublishOpReplaceContent,
			SourceKind:  types.FeishuPublishSourceKindKnowledge,
			SourceID:    k.ID,
			Title:       title,
			UploadBytes: k.Length,
		})
	}
	mappedTitle := m.Title
	if mappedTitle != title {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpRename,
			SourceKind: types.FeishuPublishSourceKindKnowledge,
			SourceID:   k.ID,
			Title:      title,
			Detail:     fmt.Sprintf("title %q → %q", mappedTitle, title),
		})
	}
	expectedParent := expectedParentNodeToken(parentKind, parentID, "", nodeByKey)
	if m.ParentNodeToken != expectedParent {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpMove,
			SourceKind: types.FeishuPublishSourceKindKnowledge,
			SourceID:   k.ID,
			Title:      title,
			Detail:     fmt.Sprintf("parent %q → %q", m.ParentNodeToken, expectedParent),
		})
	}
	if len(ops) == 0 {
		ops = append(ops, types.FeishuPublishPlanOp{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: types.FeishuPublishSourceKindKnowledge,
			SourceID:   k.ID,
			Title:      title,
		})
	}
	return ops
}

func planMissingMapped(m *types.FeishuPublishMapping) []types.FeishuPublishPlanOp {
	if m == nil {
		return nil
	}
	switch m.Status {
	case types.FeishuPublishMappingConflict:
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpRetire,
			SourceKind: m.SourceKind,
			SourceID:   m.SourceID,
			Title:      m.Title,
			Detail:     "overwrite conflict",
		}}
	case types.FeishuPublishMappingArchived:
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: m.SourceKind,
			SourceID:   m.SourceID,
			Title:      m.Title,
			Detail:     "already archived",
		}}
	case types.FeishuPublishMappingActive:
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpRetire,
			SourceKind: m.SourceKind,
			SourceID:   m.SourceID,
			Title:      m.Title,
		}}
	default:
		return []types.FeishuPublishPlanOp{{
			Op:         types.FeishuPublishOpSkip,
			SourceKind: m.SourceKind,
			SourceID:   m.SourceID,
			Title:      m.Title,
			Detail:     "unknown mapping status",
		}}
	}
}

func expectedParentNodeToken(kind, id, legacyDirectoryParent string, nodeByKey map[mappingKey]string) string {
	kind, id = ResolveFeishuPublishParent(kind, id, legacyDirectoryParent)
	if kind == types.FeishuPublishSourceKindHome || kind == "" {
		return nodeByKey[mappingKey{Kind: types.FeishuPublishSourceKindHome, ID: feishuPublishManagedHomeSourceID}]
	}
	return nodeByKey[mappingKey{Kind: kind, ID: id}]
}

func orderNavTopo(nodes []types.FeishuPublishNavSnap) []types.FeishuPublishNavSnap {
	if len(nodes) <= 1 {
		return nodes
	}
	byID := map[string]types.FeishuPublishNavSnap{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	visited := map[string]bool{}
	var out []types.FeishuPublishNavSnap
	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		n, ok := byID[id]
		if !ok {
			return
		}
		visited[id] = true
		if n.ExpectedParentKind == types.FeishuPublishSourceKindTag && n.ExpectedParentID != "" {
			visit(n.ExpectedParentID)
		}
		out = append(out, n)
	}
	for _, n := range nodes {
		visit(n.ID)
	}
	return out
}

func orderDirectoriesTopo(dirs []types.FeishuPublishDirectorySnap) []types.FeishuPublishDirectorySnap {
	if len(dirs) <= 1 {
		return dirs
	}
	byID := map[string]types.FeishuPublishDirectorySnap{}
	for _, d := range dirs {
		byID[d.ID] = d
	}
	visited := map[string]bool{}
	var out []types.FeishuPublishDirectorySnap
	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		d, ok := byID[id]
		if !ok {
			return
		}
		visited[id] = true
		kind, parentID := ResolveFeishuPublishParent(d.ExpectedParentKind, d.ExpectedParentID, d.ExpectedParent)
		if kind == types.FeishuPublishSourceKindDirectory && parentID != "" {
			visit(parentID)
		}
		out = append(out, d)
	}
	for _, d := range dirs {
		visit(d.ID)
	}
	return out
}

func applyParentBlocking(snapshot *types.FeishuPublishSnapshotPayload, ops []types.FeishuPublishPlanOp) []types.FeishuPublishPlanOp {
	if len(ops) == 0 {
		return ops
	}
	parentOf := map[mappingKey]mappingKey{}
	for _, v := range snapshot.VirtualFolders {
		child := mappingKey{Kind: types.FeishuPublishSourceKindVirtualFolder, ID: v.ID}
		kind, id := ResolveFeishuPublishParent(v.ExpectedParentKind, v.ExpectedParentID, "")
		parentOf[child] = mappingKey{Kind: kind, ID: id}
	}
	for _, tag := range snapshot.Tags {
		child := mappingKey{Kind: types.FeishuPublishSourceKindTag, ID: tag.ID}
		kind, id := ResolveFeishuPublishParent(tag.ExpectedParentKind, tag.ExpectedParentID, "")
		parentOf[child] = mappingKey{Kind: kind, ID: id}
	}
	for _, d := range snapshot.Directories {
		child := mappingKey{Kind: types.FeishuPublishSourceKindDirectory, ID: d.ID}
		kind, id := ResolveFeishuPublishParent(d.ExpectedParentKind, d.ExpectedParentID, d.ExpectedParent)
		parentOf[child] = mappingKey{Kind: kind, ID: id}
	}
	for _, k := range snapshot.Knowledge {
		if k.Eligibility != types.FeishuPublishEligibilityPresent {
			continue
		}
		child := mappingKey{Kind: types.FeishuPublishSourceKindKnowledge, ID: k.ID}
		kind, id := ResolveFeishuPublishParent(k.ExpectedParentKind, k.ExpectedParentID, "")
		parentOf[child] = mappingKey{Kind: kind, ID: id}
	}

	blocking := map[mappingKey]string{}
	for _, op := range ops {
		if op.Op == types.FeishuPublishOpConflict || op.Op == types.FeishuPublishOpBlocked {
			blocking[mappingKey{Kind: op.SourceKind, ID: op.SourceID}] = op.Op
		}
	}

	changed := true
	for changed {
		changed = false
		for i := range ops {
			op := &ops[i]
			if op.Op == types.FeishuPublishOpConflict || op.Op == types.FeishuPublishOpBlocked ||
				op.Op == types.FeishuPublishOpRetire || op.Op == types.FeishuPublishOpSkip {
				continue
			}
			parent, ok := parentOf[mappingKey{Kind: op.SourceKind, ID: op.SourceID}]
			if !ok {
				continue
			}
			if reason, blocked := blocking[parent]; blocked {
				op.Op = types.FeishuPublishOpBlocked
				op.BlockedBy = parent.Kind + ":" + parent.ID
				op.Detail = "blocked by parent " + reason
				op.UploadBytes = 0
				blocking[mappingKey{Kind: op.SourceKind, ID: op.SourceID}] = types.FeishuPublishOpBlocked
				changed = true
			}
		}
	}
	return ops
}

func aggregateFeishuPublishCounts(ops []types.FeishuPublishPlanOp) types.FeishuPublishCounts {
	var counts types.FeishuPublishCounts
	for _, op := range ops {
		switch op.Op {
		case types.FeishuPublishOpCreate:
			counts.Create++
		case types.FeishuPublishOpReplaceContent, types.FeishuPublishOpRename:
			counts.Update++
		case types.FeishuPublishOpMove:
			counts.Move++
		case types.FeishuPublishOpRetire:
			counts.Retire++
		case types.FeishuPublishOpRestore:
			counts.Restore++
		case types.FeishuPublishOpSkip:
			counts.Skip++
		case types.FeishuPublishOpConflict:
			counts.Conflict++
		case types.FeishuPublishOpBlocked:
			counts.Blocked++
		}
		counts.UploadBytes += op.UploadBytes
	}
	return counts
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
