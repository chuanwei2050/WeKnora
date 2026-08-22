package types

import (
	"testing"
	"time"
)

func TestKnowledgeVersionLifecycle(t *testing.T) {
	version := &KnowledgeVersion{Status: KnowledgeVersionDraft}
	for _, status := range []KnowledgeVersionStatus{KnowledgeVersionPendingReview, KnowledgeVersionApproved, KnowledgeVersionIndexing, KnowledgeVersionActive, KnowledgeVersionSuperseded} {
		if err := TransitionKnowledgeVersion(version, status); err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
	}
	if err := TransitionKnowledgeVersion(version, KnowledgeVersionActive); err == nil {
		t.Fatal("expected superseded -> active to be rejected")
	}
}

func TestKnowledgeVersionVisibilityAndOverlap(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := now.Add(24 * time.Hour)
	version := KnowledgeVersion{ID: "v1", Status: KnowledgeVersionActive, EffectiveAt: &now, ExpiresAt: &expires}
	if !version.IsRetrievable(now.Add(time.Hour)) {
		t.Fatal("active version should be retrievable inside its validity window")
	}
	candidateStart := now.Add(12 * time.Hour)
	candidateEnd := now.Add(48 * time.Hour)
	candidate := KnowledgeVersion{ID: "v2", EffectiveAt: &candidateStart, ExpiresAt: &candidateEnd}
	if err := ValidateVersionValidityWindow(candidate, []*KnowledgeVersion{&version}); err == nil {
		t.Fatal("expected overlapping version window to be rejected")
	}
}

func TestStandardMetadataRequiresVersion(t *testing.T) {
	metadata := KnowledgeSourceMetadata{Layer: KnowledgeLayerStandard, SourceCategory: "national_standard", AuthorityLevel: "high"}
	if err := metadata.Validate(); err == nil {
		t.Fatal("expected standard metadata validation failure")
	}
}

func TestKnowledgeVersionUniquenessUsesMetadataValues(t *testing.T) {
	firstEffective := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	secondEffective := firstEffective
	metadata := KnowledgeSourceMetadata{
		Layer: KnowledgeLayerStandard, SourceCategory: "national_standard",
		StandardNumber: "GB-1", VersionLabel: "2026", AuthorityLevel: "high",
		EffectiveAt: &firstEffective,
	}
	existing := &KnowledgeVersion{ContentHash: HashKnowledgeContent([]byte("same")), SourceMetadata: metadata}
	copyMetadata := metadata
	copyMetadata.EffectiveAt = &secondEffective
	duplicate, err := ValidateVersionUniqueness(existing, existing.ContentHash, copyMetadata)
	if err != nil || !duplicate {
		t.Fatalf("expected equal metadata values to be duplicate, duplicate=%v err=%v", duplicate, err)
	}
	if _, err := ValidateVersionUniqueness(existing, "not-a-hash", metadata); err == nil {
		t.Fatal("expected invalid content hash to be rejected")
	}
}

func TestKnowledgeVersionRetrievalBoundaries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := now.Add(time.Hour)
	expired := now.Add(-time.Hour)
	for name, version := range map[string]KnowledgeVersion{
		"not active":    {Status: KnowledgeVersionScheduled, EffectiveAt: &expired},
		"not effective": {Status: KnowledgeVersionActive, EffectiveAt: &before},
		"expired":       {Status: KnowledgeVersionActive, ExpiresAt: &expired},
	} {
		if version.IsRetrievable(now) {
			t.Errorf("%s version must not be retrievable", name)
		}
	}
}

func TestKnowledgeVersionTransitionMatrix(t *testing.T) {
	valid := map[KnowledgeVersionStatus][]KnowledgeVersionStatus{
		KnowledgeVersionDraft:         {KnowledgeVersionPendingReview, KnowledgeVersionRejected},
		KnowledgeVersionPendingReview: {KnowledgeVersionDraft, KnowledgeVersionApproved, KnowledgeVersionRejected},
		KnowledgeVersionApproved:      {KnowledgeVersionIndexing},
		KnowledgeVersionIndexing:      {KnowledgeVersionActive, KnowledgeVersionScheduled, KnowledgeVersionPublishFailed},
		KnowledgeVersionScheduled:     {KnowledgeVersionActive, KnowledgeVersionExpired},
		KnowledgeVersionActive:        {KnowledgeVersionSuperseded, KnowledgeVersionExpired},
		KnowledgeVersionPublishFailed: {KnowledgeVersionIndexing},
		KnowledgeVersionSuperseded:    {KnowledgeVersionIndexing},
		KnowledgeVersionRejected:      {KnowledgeVersionDraft},
	}
	for from, destinations := range valid {
		for _, to := range destinations {
			if !CanTransitionKnowledgeVersion(from, to) {
				t.Errorf("expected transition %s -> %s to be valid", from, to)
			}
		}
	}
	for _, from := range []KnowledgeVersionStatus{KnowledgeVersionExpired} {
		if CanTransitionKnowledgeVersion(from, KnowledgeVersionIndexing) {
			t.Errorf("terminal status %s must not transition", from)
		}
	}
	if CanTransitionKnowledgeVersion(KnowledgeVersionActive, KnowledgeVersionApproved) || CanTransitionKnowledgeVersion(KnowledgeVersionDraft, KnowledgeVersionActive) {
		t.Fatal("invalid lifecycle transitions must be rejected")
	}
	if CanTransitionKnowledgeVersion(KnowledgeVersionDraft, KnowledgeVersionStatus("unknown")) {
		t.Fatal("unknown lifecycle status must be rejected")
	}
}

func TestKnowledgeVersionReviewRejectsSelfApprovalAndRejection(t *testing.T) {
	version := &KnowledgeVersion{CreatedBy: " reviewer-1 "}
	for _, next := range []KnowledgeVersionStatus{KnowledgeVersionApproved, KnowledgeVersionRejected} {
		if err := ValidateKnowledgeVersionReview(version, "reviewer-1", next); err == nil {
			t.Fatalf("expected self-review rejection for %s", next)
		}
	}
	if err := ValidateKnowledgeVersionReview(version, "reviewer-2", KnowledgeVersionApproved); err != nil {
		t.Fatal(err)
	}
	if err := ValidateKnowledgeVersionReview(version, "reviewer-1", KnowledgeVersionPendingReview); err != nil {
		t.Fatalf("submitter should be allowed to submit their version: %v", err)
	}
}

func TestKnowledgeGovernanceConfigRequiresProfileSnapshot(t *testing.T) {
	if err := (KnowledgeGovernanceConfig{Enabled: true}).Validate(); err == nil {
		t.Fatal("enabled governance must require a profile snapshot")
	}
	if err := (KnowledgeGovernanceConfig{Enabled: true, ProfileID: "software-testing"}).Validate(); err == nil {
		t.Fatal("enabled governance must require a profile version")
	}
	if err := (KnowledgeGovernanceConfig{Enabled: true, ProfileID: "software-testing", ProfileVersion: "1.0.0"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (KnowledgeGovernanceConfig{}).Validate(); err != nil {
		t.Fatal(err)
	}
}
