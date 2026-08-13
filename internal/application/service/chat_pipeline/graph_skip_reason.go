package chatpipeline

import "github.com/Tencent/WeKnora/internal/types"

// GraphSkipAssessment captures layer-1/2 skip reasons for telemetry.
type GraphSkipAssessment struct {
	Layer1Allowed bool
	Reason        string
	ReasonLegacy  string
}

const (
	GraphReasonRelationNotNeeded     = "relation_not_needed"
	GraphReasonRoutingBudgetDisabled = "routing_budget_disabled"
	GraphReasonNoGraphKB             = "no_graph_kb"
	GraphReasonNoEntities            = "no_entities"
	GraphReasonNoGraphEvidence       = "no_graph_evidence"
	GraphReasonGraphEvidenceReturned = "graph_evidence_returned"
	// GraphReasonLegacyCombined is retained for acceptance scripts.
	GraphReasonLegacyCombined = "routing_disabled_or_relation_not_needed"
	GraphReasonLegacyNoEvidence = "relation_requested_but_no_graph_evidence"
)

// AssessGraphSkip returns precise skip reason plus a legacy-compatible string.
func AssessGraphSkip(chatManage *types.ChatManage) GraphSkipAssessment {
	layer1 := ShouldUseGraph(chatManage)
	out := GraphSkipAssessment{Layer1Allowed: layer1}
	if !layer1 {
		if chatManage != nil && chatManage.RoutingDecision != nil {
			if !chatManage.RoutingDecision.Budget.GraphEnabled {
				out.Reason = GraphReasonRoutingBudgetDisabled
			} else {
				out.Reason = GraphReasonRelationNotNeeded
			}
			out.ReasonLegacy = GraphReasonLegacyCombined
			return out
		}
		out.Reason = GraphReasonRelationNotNeeded
		out.ReasonLegacy = GraphReasonRelationNotNeeded
		return out
	}

	hasGraphKB := false
	hasEntities := false
	if chatManage != nil {
		hasGraphKB = len(chatManage.EntityKBIDs) > 0 || len(chatManage.EntityKnowledge) > 0
		hasEntities = len(chatManage.Entity) > 0
	}
	if !hasGraphKB {
		out.Reason = GraphReasonNoGraphKB
		out.ReasonLegacy = GraphReasonNoGraphKB
		return out
	}
	if !hasEntities {
		out.Reason = GraphReasonNoEntities
		out.ReasonLegacy = GraphReasonNoEntities
		return out
	}
	return out
}

// ResolveGraphTelemetryReason picks final graph reason after search completes.
func ResolveGraphTelemetryReason(requested bool, used bool, skip GraphSkipAssessment) (reason string, reasonLegacy string) {
	if used {
		return GraphReasonGraphEvidenceReturned, GraphReasonGraphEvidenceReturned
	}
	if requested {
		if skip.Reason == GraphReasonNoGraphKB || skip.Reason == GraphReasonNoEntities {
			return skip.Reason, skip.ReasonLegacy
		}
		return GraphReasonNoGraphEvidence, GraphReasonLegacyNoEvidence
	}
	if skip.Reason == "" {
		return GraphReasonRelationNotNeeded, GraphReasonLegacyCombined
	}
	return skip.Reason, skip.ReasonLegacy
}
