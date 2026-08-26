package searchutil

// SplitBudget assigns a stable share of a request-wide budget to one task.
// Every explicit scope receives at least one candidate; when tasks exceed the
// configured budget, coverage takes precedence and the merged result is capped
// by the caller after retrieval.
func SplitBudget(total, tasks, index int) int {
	if total <= 0 || tasks <= 0 || index < 0 || index >= tasks {
		return 0
	}
	if total < tasks {
		return 1
	}
	share := total / tasks
	if index < total%tasks {
		share++
	}
	return share
}
