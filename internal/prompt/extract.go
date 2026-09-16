package prompt

// Table metadata description prompts for structured/tabular knowledge.
const (
	TableDescription = `Data analysis expert. From table structure + samples, write concise metadata (200-300 words).

Table name: %s

%s

%s

Cover: subject; 3-5 core fields; scale; business scenarios; key traits (geo/category/hierarchy…).
Rules: no specific sample values; retrieval-oriented; same language as the data.`

	ColumnDescriptions = `Data analysis expert. Describe each column from structure + samples.

Table name: %s

%s

%s

Per column: meaning; type/format; business purpose; traits (unique/nullable/enums/units…).
Format:
**Column** (type)
- Field Meaning: …
- Business Purpose: …
- Data Characteristics: …

Rules: metadata only (no sample values); infer enums when possible; same language as the data.`
)
