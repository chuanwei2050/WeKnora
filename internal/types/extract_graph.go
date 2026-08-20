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
	Description    string                        `json:"description"`
	Tags           []string                      `json:"tags"`
	EntityTypes    []string                      `json:"entity_types,omitempty"`
	EntitySchema   []GraphEntityTypeDefinition   `json:"entity_schema,omitempty"`
	RelationSchema []GraphRelationTypeDefinition `json:"relation_schema,omitempty"`
	StrictSchema   bool                          `json:"strict_schema,omitempty"`
	MaxEntities    int                           `json:"max_entities,omitempty"`
	MaxRelations   int                           `json:"max_relations,omitempty"`
	MinConfidence  float64                       `json:"min_confidence,omitempty"`
	Examples       []GraphData                   `json:"examples"`
}

// GraphEntityTypeDefinition is an actual extraction schema entry, not a few-shot entity instance.
type GraphEntityTypeDefinition struct {
	Type        string `json:"type"`
	BaseType    string `json:"base_type,omitempty"`
	Description string `json:"description,omitempty"`
}

// GraphRelationTypeDefinition defines a directed semantic relation allowed by the extractor.
type GraphRelationTypeDefinition struct {
	Type        string `json:"type"`
	SourceType  string `json:"source_type"`
	TargetType  string `json:"target_type"`
	Description string `json:"description,omitempty"`
}

type GraphNode struct {
	Name        string   `json:"name,omitempty"`
	EntityType  string   `json:"entity_type,omitempty"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Chunks      []string `json:"chunks,omitempty"`
	Attributes  []string `json:"attributes,omitempty"`
}

// GraphRelation represents the relation of the graph
type GraphRelation struct {
	Node1       string  `json:"node1,omitempty"`
	Node2       string  `json:"node2,omitempty"`
	Type        string  `json:"type,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Description string  `json:"description,omitempty"`
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
