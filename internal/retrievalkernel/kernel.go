package retrievalkernel

type Outcome string

const (
	OutcomeSuccess          Outcome = "success"
	OutcomeNoRelevantResult Outcome = "no_relevant_result"
	OutcomeUnavailable      Outcome = "unavailable"
	OutcomeInvalidCandidate Outcome = "invalid_candidate"
)

func ClassifyRerank(resultCount int, err error, hasValidCandidates bool) Outcome {
	if !hasValidCandidates {
		return OutcomeInvalidCandidate
	}
	if err != nil {
		return OutcomeUnavailable
	}
	if resultCount == 0 {
		return OutcomeNoRelevantResult
	}
	return OutcomeSuccess
}
