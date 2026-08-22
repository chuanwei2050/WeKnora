package types

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type KnowledgeLayer string

const (
	KnowledgeLayerStandard   KnowledgeLayer = "standard"
	KnowledgeLayerFoundation KnowledgeLayer = "foundation"
	KnowledgeLayerInternal   KnowledgeLayer = "internal"
	KnowledgeLayerExperience KnowledgeLayer = "experience"
)

type KnowledgeVersionStatus string

const (
	KnowledgeVersionDraft         KnowledgeVersionStatus = "draft"
	KnowledgeVersionPendingReview KnowledgeVersionStatus = "pending_review"
	KnowledgeVersionApproved      KnowledgeVersionStatus = "approved"
	KnowledgeVersionIndexing      KnowledgeVersionStatus = "indexing"
	KnowledgeVersionScheduled     KnowledgeVersionStatus = "scheduled"
	KnowledgeVersionActive        KnowledgeVersionStatus = "active"
	KnowledgeVersionPublishFailed KnowledgeVersionStatus = "publish_failed"
	KnowledgeVersionSuperseded    KnowledgeVersionStatus = "superseded"
	KnowledgeVersionRejected      KnowledgeVersionStatus = "rejected"
	KnowledgeVersionExpired       KnowledgeVersionStatus = "expired"
)

type KnowledgeGovernanceConfig struct {
	Enabled        bool   `json:"enabled" yaml:"enabled"`
	ProfileID      string `json:"profile_id,omitempty" yaml:"profile_id,omitempty"`
	ProfileVersion string `json:"profile_version,omitempty" yaml:"profile_version,omitempty"`
}

func (c KnowledgeGovernanceConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.ProfileID) == "" {
		return fmt.Errorf("governance profile_id is required when governance is enabled")
	}
	if strings.TrimSpace(c.ProfileVersion) == "" {
		return fmt.Errorf("governance profile_version is required when governance is enabled")
	}
	return nil
}

func (c KnowledgeGovernanceConfig) Value() (driver.Value, error) { return json.Marshal(c) }

func (c *KnowledgeGovernanceConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported governance value %T", value)
	}
	return json.Unmarshal(data, c)
}

type KnowledgeSourceMetadata struct {
	Layer          KnowledgeLayer `json:"layer" yaml:"layer"`
	SourceCategory string         `json:"source_category" yaml:"source_category"`
	StandardNumber string         `json:"standard_number,omitempty" yaml:"standard_number,omitempty"`
	VersionLabel   string         `json:"version_label" yaml:"version_label"`
	AuthorityLevel string         `json:"authority_level" yaml:"authority_level"`
	Department     string         `json:"department,omitempty" yaml:"department,omitempty"`
	EffectiveAt    *time.Time     `json:"effective_at,omitempty" yaml:"effective_at,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

func (m KnowledgeSourceMetadata) Value() (driver.Value, error) { return json.Marshal(m) }

func (m *KnowledgeSourceMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("unsupported source metadata value %T", value)
	}
	return json.Unmarshal(data, m)
}

func (m KnowledgeSourceMetadata) Validate() error {
	switch m.Layer {
	case KnowledgeLayerStandard:
		if strings.TrimSpace(m.StandardNumber) == "" {
			return fmt.Errorf("standard_number is required for standard knowledge")
		}
		if strings.TrimSpace(m.VersionLabel) == "" {
			return fmt.Errorf("version_label is required for standard knowledge")
		}
	case KnowledgeLayerInternal:
		if strings.TrimSpace(m.Department) == "" {
			return fmt.Errorf("department is required for internal knowledge")
		}
	case KnowledgeLayerFoundation, KnowledgeLayerExperience:
	default:
		return fmt.Errorf("unknown knowledge layer %q", m.Layer)
	}
	if strings.TrimSpace(m.SourceCategory) == "" {
		return fmt.Errorf("source_category is required")
	}
	if strings.TrimSpace(m.AuthorityLevel) == "" {
		return fmt.Errorf("authority_level is required")
	}
	if m.EffectiveAt != nil && m.ExpiresAt != nil && !m.EffectiveAt.Before(*m.ExpiresAt) {
		return fmt.Errorf("effective_at must be before expires_at")
	}
	return nil
}

type KnowledgeVersion struct {
	ID                string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID          uint64                  `json:"tenant_id" gorm:"index"`
	KnowledgeID       string                  `json:"knowledge_id" gorm:"index"`
	VersionLabel      string                  `json:"version_label"`
	ContentHash       string                  `json:"content_hash" gorm:"index"`
	SnapshotRef       string                  `json:"snapshot_ref,omitempty"`
	SourceMetadata    KnowledgeSourceMetadata `json:"source_metadata" gorm:"type:json"`
	PreviousVersionID string                  `json:"previous_version_id,omitempty"`
	Status            KnowledgeVersionStatus  `json:"status" gorm:"index"`
	CreatedBy         string                  `json:"created_by"`
	CreatedAt         time.Time               `json:"created_at"`
	EffectiveAt       *time.Time              `json:"effective_at,omitempty"`
	ExpiresAt         *time.Time              `json:"expires_at,omitempty"`
}

func (v KnowledgeVersion) IsRetrievable(now time.Time) bool {
	if v.Status != KnowledgeVersionActive {
		return false
	}
	if v.EffectiveAt != nil && now.Before(*v.EffectiveAt) {
		return false
	}
	return v.ExpiresAt == nil || now.Before(*v.ExpiresAt)
}

func ValidateVersionValidityWindow(candidate KnowledgeVersion, existing []*KnowledgeVersion) error {
	if candidate.EffectiveAt == nil && candidate.ExpiresAt == nil {
		return nil
	}
	if candidate.EffectiveAt != nil && candidate.ExpiresAt != nil && !candidate.EffectiveAt.Before(*candidate.ExpiresAt) {
		return fmt.Errorf("effective_at must be before expires_at")
	}
	for _, version := range existing {
		if version == nil || version.ID == candidate.ID || (version.EffectiveAt == nil && version.ExpiresAt == nil) {
			continue
		}
		candidateStartsBeforeExistingEnds := candidate.ExpiresAt == nil || version.EffectiveAt == nil || candidate.ExpiresAt.After(*version.EffectiveAt)
		existingStartsBeforeCandidateEnds := version.ExpiresAt == nil || candidate.EffectiveAt == nil || version.ExpiresAt.After(*candidate.EffectiveAt)
		if candidateStartsBeforeExistingEnds && existingStartsBeforeCandidateEnds {
			return fmt.Errorf("validity window overlaps version %q", version.ID)
		}
	}
	return nil
}

type KnowledgeVersionReview struct {
	ID         string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	VersionID  string    `json:"version_id" gorm:"index"`
	ReviewerID string    `json:"reviewer_id"`
	Action     string    `json:"action"`
	Comment    string    `json:"comment,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func HashKnowledgeContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func IsKnowledgeVersionStatus(value KnowledgeVersionStatus) bool {
	switch value {
	case KnowledgeVersionDraft, KnowledgeVersionPendingReview, KnowledgeVersionApproved, KnowledgeVersionIndexing, KnowledgeVersionScheduled, KnowledgeVersionActive, KnowledgeVersionPublishFailed, KnowledgeVersionSuperseded, KnowledgeVersionRejected, KnowledgeVersionExpired:
		return true
	default:
		return false
	}
}

func CanTransitionKnowledgeVersion(from, to KnowledgeVersionStatus) bool {
	switch from {
	case KnowledgeVersionDraft:
		return to == KnowledgeVersionPendingReview || to == KnowledgeVersionRejected
	case KnowledgeVersionPendingReview:
		return to == KnowledgeVersionApproved || to == KnowledgeVersionRejected || to == KnowledgeVersionDraft
	case KnowledgeVersionApproved:
		return to == KnowledgeVersionIndexing
	case KnowledgeVersionIndexing:
		return to == KnowledgeVersionActive || to == KnowledgeVersionScheduled || to == KnowledgeVersionPublishFailed
	case KnowledgeVersionScheduled:
		return to == KnowledgeVersionActive || to == KnowledgeVersionExpired
	case KnowledgeVersionActive:
		return to == KnowledgeVersionSuperseded || to == KnowledgeVersionExpired
	case KnowledgeVersionPublishFailed:
		return to == KnowledgeVersionIndexing
	case KnowledgeVersionSuperseded:
		return to == KnowledgeVersionIndexing
	case KnowledgeVersionRejected:
		return to == KnowledgeVersionDraft
	case KnowledgeVersionExpired:
		return false
	default:
		return false
	}
}

func TransitionKnowledgeVersion(version *KnowledgeVersion, next KnowledgeVersionStatus) error {
	if version == nil {
		return fmt.Errorf("knowledge version is required")
	}
	if !IsKnowledgeVersionStatus(next) {
		return fmt.Errorf("unknown knowledge version status %q", next)
	}
	if !CanTransitionKnowledgeVersion(version.Status, next) {
		return fmt.Errorf("invalid knowledge version transition %s -> %s", version.Status, next)
	}
	version.Status = next
	return nil
}

// ValidateKnowledgeVersionReview enforces the reviewer's separation-of-duties
// rule before an approval or rejection is recorded.
func ValidateKnowledgeVersionReview(version *KnowledgeVersion, reviewerID string, next KnowledgeVersionStatus) error {
	if version == nil {
		return fmt.Errorf("knowledge version is required")
	}
	if (next == KnowledgeVersionApproved || next == KnowledgeVersionRejected) &&
		strings.TrimSpace(version.CreatedBy) != "" && strings.TrimSpace(version.CreatedBy) == strings.TrimSpace(reviewerID) {
		return fmt.Errorf("the submitter cannot review their own version")
	}
	return nil
}

// ValidateVersionUniqueness prevents a repeated source sync from creating a
// new immutable version when both content and governed metadata are unchanged.
func ValidateVersionUniqueness(existing *KnowledgeVersion, contentHash string, metadata KnowledgeSourceMetadata) (bool, error) {
	if err := metadata.Validate(); err != nil {
		return false, err
	}
	contentHash = strings.ToLower(strings.TrimSpace(contentHash))
	if len(contentHash) != sha256.Size*2 {
		return false, fmt.Errorf("content_hash must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(contentHash); err != nil {
		return false, fmt.Errorf("content_hash must be a SHA-256 hex digest: %w", err)
	}
	if existing == nil {
		return false, nil
	}
	left, err := json.Marshal(existing.SourceMetadata)
	if err != nil {
		return false, err
	}
	right, err := json.Marshal(metadata)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(existing.ContentHash, contentHash) && string(left) == string(right), nil
}
