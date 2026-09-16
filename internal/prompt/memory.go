package prompt

// Conversation-memory graph extraction (schema: summary + weight).
const ExtractMemoryGraph = `Extract a knowledge graph from the conversation. JSON only:
{"summary":"...","entities":[{"title":"...","type":"Person|Location|Concept|...","description":"..."}],"relationships":[{"source":"...","target":"...","description":"...","weight":1.0}]}

Conversation:
%s
`

// ExtractMemoryKeywords extracts search keywords for the memory graph.
const ExtractMemoryKeywords = `Extract search keywords for a knowledge graph. JSON only:
{"keywords":["keyword1","keyword2"]}

Query:
%s
`
