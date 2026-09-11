package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

// IsFeishuKnowledgeSyncEnabled reports whether the Feishu knowledge sync feature flag is on.
func IsFeishuKnowledgeSyncEnabled() bool {
	return types.IsFeishuKnowledgeSyncEnabled()
}

// BuildSnapshotPayload builds an immutable Feishu publish snapshot from KB name, tags,
// directories, knowledge rows, previously mapped sources, and optional body text by knowledge ID.
// Digest is sha256 hex of the canonical JSON encoding of the payload.
func BuildSnapshotPayload(
	kbName string,
	tags []*types.KnowledgeTag,
	dirs []*types.KnowledgeDirectory,
	knowledge []*types.Knowledge,
	mapped []*types.FeishuPublishMappedSource,
	bodyByKnowledgeID map[string]string,
) (*types.FeishuPublishSnapshotPayload, string, error) {
	payload := &types.FeishuPublishSnapshotPayload{
		KnowledgeBaseName: strings.TrimSpace(kbName),
		VirtualFolders: []types.FeishuPublishNavSnap{
			{
				ID:   types.FeishuPublishVirtualPublicID,
				Name: types.IntegrationPublicFolderName,
			},
			{
				ID:   types.FeishuPublishVirtualUncategorizedID,
				Name: types.FeishuPublishVirtualUncategorizedTitle,
			},
		},
		Tags:        make([]types.FeishuPublishNavSnap, 0, len(tags)),
		Directories: make([]types.FeishuPublishDirectorySnap, 0, len(dirs)),
		Knowledge:   make([]types.FeishuPublishKnowledgeSnap, 0, len(knowledge)+len(mapped)),
		MappedIDs:   make([]types.FeishuPublishMappedSource, 0, len(mapped)),
	}

	tagIDs := map[string]struct{}{}
	for _, tag := range tags {
		if tag == nil || strings.TrimSpace(tag.ID) == "" {
			continue
		}
		tagIDs[tag.ID] = struct{}{}
		parentKind, parentID := tagExpectedParent(tag, tagIDs)
		payload.Tags = append(payload.Tags, types.FeishuPublishNavSnap{
			ID:                 tag.ID,
			Name:               tag.Name,
			ExpectedParentKind: parentKind,
			ExpectedParentID:   parentID,
		})
	}
	// Second pass: parents that appear later in the list still resolve correctly.
	for i := range payload.Tags {
		tag := findTag(tags, payload.Tags[i].ID)
		if tag == nil {
			continue
		}
		payload.Tags[i].ExpectedParentKind, payload.Tags[i].ExpectedParentID = tagExpectedParent(tag, tagIDs)
	}

	for _, d := range dirs {
		if d == nil {
			continue
		}
		if d.Status != "" && d.Status != types.DirectoryStatusActive {
			continue
		}
		parentID := ""
		if d.ParentID != nil {
			parentID = *d.ParentID
		}
		parentKind, expectedParentID := directoryExpectedParent(d, parentID, tagIDs)
		payload.Directories = append(payload.Directories, types.FeishuPublishDirectorySnap{
			ID:                 d.ID,
			ParentID:           parentID,
			TagID:              d.TagID,
			Name:               d.Name,
			ExpectedParentKind: parentKind,
			ExpectedParentID:   expectedParentID,
		})
	}

	presentIDs := map[string]struct{}{}
	for _, k := range knowledge {
		if k == nil {
			continue
		}
		if k.DeletedAt.Valid {
			continue
		}
		presentIDs[k.ID] = struct{}{}
		body := ""
		if bodyByKnowledgeID != nil {
			body = bodyByKnowledgeID[k.ID]
		}
		payload.Knowledge = append(payload.Knowledge, buildKnowledgeSnap(k, body, tagIDs))
	}

	for _, m := range mapped {
		if m == nil {
			continue
		}
		payload.MappedIDs = append(payload.MappedIDs, *m)
		if m.SourceKind != types.FeishuPublishSourceKindKnowledge {
			continue
		}
		if _, ok := presentIDs[m.SourceID]; ok {
			continue
		}
		payload.Knowledge = append(payload.Knowledge, types.FeishuPublishKnowledgeSnap{
			ID:          m.SourceID,
			Eligibility: types.FeishuPublishEligibilityDeleted,
			SkipReason:  "authoritative deletion",
		})
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("marshal feishu publish snapshot: %w", err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	return payload, digest, nil
}

func findTag(tags []*types.KnowledgeTag, id string) *types.KnowledgeTag {
	for _, t := range tags {
		if t != nil && t.ID == id {
			return t
		}
	}
	return nil
}

func tagExpectedParent(tag *types.KnowledgeTag, knownTags map[string]struct{}) (kind, id string) {
	if tag.ParentID != nil && strings.TrimSpace(*tag.ParentID) != "" {
		parentID := strings.TrimSpace(*tag.ParentID)
		if _, ok := knownTags[parentID]; ok {
			return types.FeishuPublishSourceKindTag, parentID
		}
	}
	if tag.IsPublic {
		return types.FeishuPublishSourceKindVirtualFolder, types.FeishuPublishVirtualPublicID
	}
	return types.FeishuPublishSourceKindHome, feishuPublishManagedHomeSourceID
}

func directoryExpectedParent(d *types.KnowledgeDirectory, parentID string, knownTags map[string]struct{}) (kind, id string) {
	if parentID != "" {
		return types.FeishuPublishSourceKindDirectory, parentID
	}
	if tagID := strings.TrimSpace(d.TagID); tagID != "" {
		if _, ok := knownTags[tagID]; ok {
			return types.FeishuPublishSourceKindTag, tagID
		}
	}
	return types.FeishuPublishSourceKindVirtualFolder, types.FeishuPublishVirtualUncategorizedID
}

func buildKnowledgeSnap(k *types.Knowledge, body string, knownTags map[string]struct{}) types.FeishuPublishKnowledgeSnap {
	dirID := ""
	if k.DirectoryID != nil {
		dirID = *k.DirectoryID
	}
	bodyText, truncated := truncateBody(body, types.FeishuPublishMaxBodyChars)
	bodyHash := ""
	if bodyText != "" || truncated {
		sum := sha256.Sum256([]byte(bodyText))
		bodyHash = hex.EncodeToString(sum[:])
	}

	parentKind, parentID := knowledgeExpectedParent(dirID, k.TagID, knownTags)
	snap := types.FeishuPublishKnowledgeSnap{
		ID:                 k.ID,
		DirectoryID:        dirID,
		TagID:              k.TagID,
		Title:              firstNonEmpty(k.Title, k.FileName),
		FileName:           k.FileName,
		MIME:               k.FileType,
		Length:             k.FileSize,
		ObjectKey:          k.FilePath,
		ObjectVersion:      firstNonEmpty(k.CurrentVersionID, k.FileHash),
		FileHash:           k.FileHash,
		BodyHash:           bodyHash,
		BodyText:           bodyText,
		BodyTruncated:      truncated,
		SourceVersion:      k.CurrentVersionID,
		ExpectedParentKind: parentKind,
		ExpectedParentID:   parentID,
	}

	eligibility, reason := classifyKnowledgeEligibility(k)
	snap.Eligibility = eligibility
	snap.SkipReason = reason
	if eligibility != types.FeishuPublishEligibilityPresent {
		snap.BodyText = ""
	}
	return snap
}

func knowledgeExpectedParent(dirID, tagID string, knownTags map[string]struct{}) (kind, id string) {
	if dirID != "" {
		return types.FeishuPublishSourceKindDirectory, dirID
	}
	if tagID = strings.TrimSpace(tagID); tagID != "" {
		if _, ok := knownTags[tagID]; ok {
			return types.FeishuPublishSourceKindTag, tagID
		}
	}
	return types.FeishuPublishSourceKindVirtualFolder, types.FeishuPublishVirtualUncategorizedID
}

func classifyKnowledgeEligibility(k *types.Knowledge) (eligibility string, reason string) {
	if strings.EqualFold(k.EnableStatus, "disabled") {
		return types.FeishuPublishEligibilityTemporarilyIneligible, "knowledge disabled"
	}
	if k.ParseStatus != types.ParseStatusCompleted {
		return types.FeishuPublishEligibilityTemporarilyIneligible, "parse status: " + k.ParseStatus
	}
	if strings.TrimSpace(k.FilePath) == "" {
		return types.FeishuPublishEligibilityTemporarilyIneligible, "missing file object"
	}
	if k.FileSize > types.FeishuPublishMaxUploadBytes {
		return types.FeishuPublishEligibilityTemporarilyIneligible,
			fmt.Sprintf("file too large: %.1f MB (max %d MB)", float64(k.FileSize)/(1024*1024), types.FeishuPublishMaxUploadMB)
	}
	return types.FeishuPublishEligibilityPresent, ""
}

func truncateBody(body string, maxChars int) (string, bool) {
	if maxChars <= 0 || body == "" {
		return body, false
	}
	if utf8.RuneCountInString(body) <= maxChars {
		return body, false
	}
	runes := []rune(body)
	return string(runes[:maxChars]), true
}

// ResolveFeishuPublishParent normalizes legacy directory-only parent fields.
func ResolveFeishuPublishParent(kind, id, legacyDirectoryParent string) (string, string) {
	if kind != "" || id != "" {
		if kind == "" {
			kind = types.FeishuPublishSourceKindDirectory
		}
		return kind, id
	}
	if legacyDirectoryParent != "" {
		return types.FeishuPublishSourceKindDirectory, legacyDirectoryParent
	}
	return types.FeishuPublishSourceKindHome, feishuPublishManagedHomeSourceID
}
