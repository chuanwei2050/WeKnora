package types

// TableAnalysisQuery is one business fact request evaluated against a single
// authorized structured knowledge file.
type TableAnalysisQuery struct {
	ID    string `json:"id"`
	Query string `json:"query"`
}

// TableAnalysisQueryResult contains bounded structured rows plus an internal
// query audit. Callers decide whether the audit is customer-visible.
type TableAnalysisQueryResult struct {
	ID       string              `json:"id"`
	Status   string              `json:"status"`
	Rows     []map[string]string `json:"rows"`
	RowCount int                 `json:"row_count"`
	SQL      string              `json:"sql"`
	Error    string              `json:"error,omitempty"`
}

// TableAnalysisResult is a tenant-scoped batch analysis result for one file.
type TableAnalysisResult struct {
	KnowledgeID     string                     `json:"knowledge_id"`
	KnowledgeBaseID string                     `json:"knowledge_base_id"`
	FileType        string                     `json:"file_type"`
	Filename        string                     `json:"filename"`
	Results         []TableAnalysisQueryResult `json:"results"`
}
