package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ChunkMetadataPatch is a whitelist-only metadata update applied without re-embedding.
// Only is_enabled and tag_id are allowed; content / embedding must never appear.
type ChunkMetadataPatch struct {
	IsEnabled *bool   `json:"is_enabled,omitempty"`
	TagID     *string `json:"tag_id,omitempty"`
}

// Validate ensures at least one whitelisted field is set.
func (p ChunkMetadataPatch) Validate() error {
	if p.IsEnabled == nil && p.TagID == nil {
		return fmt.Errorf("metadata patch requires at least one whitelisted field")
	}
	return nil
}

// ParseChunkMetadataPatch decodes JSON with unknown fields rejected (e.g. content, embedding).
func ParseChunkMetadataPatch(data []byte) (ChunkMetadataPatch, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p ChunkMetadataPatch
	if err := dec.Decode(&p); err != nil {
		return ChunkMetadataPatch{}, fmt.Errorf("illegal or invalid metadata field: %w", err)
	}
	if err := p.Validate(); err != nil {
		return ChunkMetadataPatch{}, err
	}
	return p, nil
}

// MergeChunkMetadataPatches merges enable/tag maps into a single patch map per chunk ID.
func MergeChunkMetadataPatches(enabled map[string]bool, tags map[string]string) map[string]ChunkMetadataPatch {
	out := make(map[string]ChunkMetadataPatch, len(enabled)+len(tags))
	for id, v := range enabled {
		val := v
		p := out[id]
		p.IsEnabled = &val
		out[id] = p
	}
	for id, tag := range tags {
		t := tag
		p := out[id]
		p.TagID = &t
		out[id] = p
	}
	return out
}

// IndexACLHotUpdateEnabled reports whether optional document-level ACL hot update is on.
// Default false (C2 stub / opt-in).
func IndexACLHotUpdateEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("INDEX_ACL_HOT_UPDATE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
