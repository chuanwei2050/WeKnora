package searchutil

import "testing"

func TestSplitBudgetPreservesRequestTotal(t *testing.T) {
	total := 0
	for i := 0; i < 3; i++ {
		total += SplitBudget(10, 3, i)
	}
	if total != 10 {
		t.Fatalf("expected allocated budget 10, got %d", total)
	}
	if SplitBudget(2, 3, 2) != 1 {
		t.Fatal("every explicit retrieval scope must receive at least one candidate")
	}
}
