package types

import "testing"

func TestValidateGraphTripleTransition(t *testing.T) {
	if err := ValidateGraphTripleTransition(GraphTriplePending, GraphTripleWritten); err != nil {
		t.Fatalf("pending->written: %v", err)
	}
	if err := ValidateGraphTripleTransition(GraphTriplePending, GraphTripleRejected); err != nil {
		t.Fatalf("pending->rejected: %v", err)
	}
	if err := ValidateGraphTripleTransition(GraphTriplePending, GraphTripleSuperseded); err != nil {
		t.Fatalf("pending->superseded: %v", err)
	}
	if err := ValidateGraphTripleTransition(GraphTripleWritten, GraphTripleRejected); err == nil {
		t.Fatal("written is terminal")
	}
	if err := ValidateGraphTripleTransition(GraphTripleRejected, GraphTripleWritten); err == nil {
		t.Fatal("rejected is terminal")
	}
	if !CanApproveGraphTriple(GraphTriplePending) || CanApproveGraphTriple(GraphTripleWritten) {
		t.Fatal("approve gate mismatch")
	}
	if !CanRejectGraphTriple(GraphTriplePending) || CanRejectGraphTriple(GraphTripleSuperseded) {
		t.Fatal("reject gate mismatch")
	}
}
