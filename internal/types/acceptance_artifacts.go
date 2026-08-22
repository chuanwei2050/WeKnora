package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AcceptanceMaterialKind string

const (
	AcceptanceMaterialSourceCode AcceptanceMaterialKind = "source_code"
	AcceptanceMaterialConfig     AcceptanceMaterialKind = "config"
	AcceptanceMaterialReport     AcceptanceMaterialKind = "report"
	AcceptanceMaterialScreenshot AcceptanceMaterialKind = "screenshot"
	AcceptanceMaterialManual     AcceptanceMaterialKind = "manual"
)

func (k AcceptanceMaterialKind) Validate() error {
	switch k {
	case AcceptanceMaterialSourceCode, AcceptanceMaterialConfig, AcceptanceMaterialReport, AcceptanceMaterialScreenshot, AcceptanceMaterialManual:
		return nil
	default:
		return fmt.Errorf("unknown acceptance material kind %q", k)
	}
}

type AcceptanceArtifact struct {
	ID          string                 `json:"id"`
	RunID       string                 `json:"run_id"`
	Kind        AcceptanceMaterialKind `json:"kind"`
	URI         string                 `json:"uri"`
	SHA256      string                 `json:"sha256"`
	Size        int64                  `json:"size"`
	ContentType string                 `json:"content_type,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

func (a AcceptanceArtifact) Validate() error {
	if strings.TrimSpace(a.RunID) == "" || strings.TrimSpace(a.URI) == "" {
		return fmt.Errorf("artifact run and URI are required")
	}
	if a.Kind != "" {
		if err := a.Kind.Validate(); err != nil {
			return err
		}
	}
	if len(strings.TrimSpace(a.SHA256)) != sha256.Size*2 {
		return fmt.Errorf("artifact SHA-256 is required")
	}
	if _, err := hex.DecodeString(strings.TrimSpace(a.SHA256)); err != nil {
		return fmt.Errorf("artifact SHA-256 is invalid: %w", err)
	}
	if a.Size < 0 {
		return fmt.Errorf("artifact size must not be negative")
	}
	return nil
}

type AcceptanceMaterialChecklistItem struct {
	Kind     AcceptanceMaterialKind `json:"kind"`
	Required bool                   `json:"required"`
	Present  bool                   `json:"present"`
	URI      string                 `json:"uri,omitempty"`
	Reason   string                 `json:"reason,omitempty"`
}

func BuildAcceptanceMaterialChecklist(artifacts []AcceptanceArtifact) []AcceptanceMaterialChecklistItem {
	provided := make(map[AcceptanceMaterialKind]AcceptanceArtifact, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Kind != "" && strings.TrimSpace(artifact.URI) != "" && strings.TrimSpace(artifact.SHA256) != "" {
			provided[artifact.Kind] = artifact
		}
	}
	items := make([]AcceptanceMaterialChecklistItem, 0, 5)
	for _, kind := range []AcceptanceMaterialKind{AcceptanceMaterialSourceCode, AcceptanceMaterialConfig, AcceptanceMaterialReport, AcceptanceMaterialScreenshot, AcceptanceMaterialManual} {
		artifact, ok := provided[kind]
		item := AcceptanceMaterialChecklistItem{Kind: kind, Required: true, Present: ok}
		if ok {
			item.URI = artifact.URI
		} else {
			item.Reason = "material is missing"
		}
		items = append(items, item)
	}
	return items
}

// AcceptanceArtifactStore is intentionally storage-provider neutral. An S3,
// MinIO or other object-store adapter can implement it without changing the
// benchmark repository contract.
type AcceptanceArtifactStore interface {
	Put(context.Context, string, io.Reader) (uri string, sha256 string, size int64, err error)
}

// FileAcceptanceArtifactStore is the local development adapter. It preserves
// the same content-addressed checksum contract and rejects path traversal.
type FileAcceptanceArtifactStore struct{ Root string }

func (s FileAcceptanceArtifactStore) Put(ctx context.Context, key string, source io.Reader) (string, string, int64, error) {
	if strings.TrimSpace(s.Root) == "" || strings.TrimSpace(key) == "" || source == nil {
		return "", "", 0, fmt.Errorf("artifact root, key and source are required")
	}
	clean := filepath.Clean(key)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", "", 0, fmt.Errorf("artifact key escapes storage root")
	}
	if err := ctx.Err(); err != nil {
		return "", "", 0, err
	}
	path := filepath.Join(s.Root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", "", 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hasher), source)
	if err != nil {
		return "", "", written, err
	}
	return path, hex.EncodeToString(hasher.Sum(nil)), written, nil
}
