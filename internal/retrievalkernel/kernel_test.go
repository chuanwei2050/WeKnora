package retrievalkernel

import (
	"errors"
	"testing"
)

func TestClassifyRerank(t *testing.T) {
	if got := ClassifyRerank(0, nil, true); got != OutcomeNoRelevantResult {
		t.Fatalf("empty result classified as %q", got)
	}
	if got := ClassifyRerank(0, errors.New("down"), true); got != OutcomeUnavailable {
		t.Fatalf("error classified as %q", got)
	}
	if got := ClassifyRerank(1, nil, false); got != OutcomeInvalidCandidate {
		t.Fatalf("invalid candidate classified as %q", got)
	}
}
