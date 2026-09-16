package types

// FilterDisabledFoldersDefault is for conversation and floating-widget chat:
// omitted means exclude folders with search turned off. Explicit false remains an opt-out.
// RAG and batch RAG keep a plain bool so callers control the flag with no product default.
func FilterDisabledFoldersDefault(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}
