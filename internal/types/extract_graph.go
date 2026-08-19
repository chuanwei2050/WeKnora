package types

// ChunkContext represents chunk content with surrounding context
type ChunkContext struct {
	ChunkID     string `json:"chunk_id"`
	Content     string `json:"content"`
	PrevContent string `json:"prev_content,omitempty"` // Previous chunk content for context
	NextContent string `json:"next_content,omitempty"` // Next chunk content for context
}

// PromptTemplateStructured represents the prompt template structured
type PromptTemplateStructured struct {
	Description   string      `json:"description"`
	Tags          []string    `json:"tags"`
	EntityTypes   []string    `json:"entity_types,omitempty"`
	StrictSchema  bool        `json:"strict_schema,omitempty"`
	MaxEntities   int         `json:"max_entities,omitempty"`
	MaxRelations  int         `json:"max_relations,omitempty"`
	MinConfidence float64     `json:"min_confidence,omitempty"`
	Examples      []GraphData `json:"examples"`
}

type GraphNode struct {
	Name       string   `json:"name,omitempty"`
	EntityType string   `json:"entity_type,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
	Chunks     []string `json:"chunks,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
}

// GraphRelation represents the relation of the graph
type GraphRelation struct {
	Node1      string  `json:"node1,omitempty"`
	Node2      string  `json:"node2,omitempty"`
	Type       string  `json:"type,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type GraphData struct {
	Text     string           `json:"text,omitempty"`
	Node     []*GraphNode     `json:"node,omitempty"`
	Relation []*GraphRelation `json:"relation,omitempty"`
}

// NameSpace represents the name space of the knowledge base and knowledge
type NameSpace struct {
	KnowledgeBase      string `json:"knowledge_base"`
	Knowledge          string `json:"knowledge"`
	KnowledgeVersionID string `json:"knowledge_version_id,omitempty"`
}

// Labels returns the labels of the name space
func (n NameSpace) Labels() []string {
	res := make([]string, 0)
	if n.KnowledgeBase != "" {
		res = append(res, n.KnowledgeBase)
	}
	if n.Knowledge != "" {
		res = append(res, n.Knowledge)
	}
	if n.KnowledgeVersionID != "" {
		res = append(res, n.KnowledgeVersionID)
	}
	return res
}
