package chatpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PluginExtractEntity is a plugin for extracting entities from user queries
// It uses historical dialog context and large language models to identify key entities in the user's original query
type PluginExtractEntity struct {
	modelService      interfaces.ModelService         // Model service for calling large language models
	template          *types.PromptTemplateStructured // Template for generating prompts
	knowledgeBaseRepo interfaces.KnowledgeBaseRepository
	knowledgeService  interfaces.KnowledgeService // For shared KB document resolution
	knowledgeRepo     interfaces.KnowledgeRepository
}

// NewPluginExtractEntity creates a new extract-entity plugin instance
// Also registers the plugin with the event manager
func NewPluginExtractEntity(
	eventManager *EventManager,
	modelService interfaces.ModelService,
	knowledgeBaseRepo interfaces.KnowledgeBaseRepository,
	knowledgeService interfaces.KnowledgeService,
	knowledgeRepo interfaces.KnowledgeRepository,
	config *config.Config,
) *PluginExtractEntity {
	res := &PluginExtractEntity{
		modelService:      modelService,
		template:          config.ExtractManager.ExtractEntity,
		knowledgeBaseRepo: knowledgeBaseRepo,
		knowledgeService:  knowledgeService,
		knowledgeRepo:     knowledgeRepo,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the list of event types this plugin responds to.
func (p *PluginExtractEntity) ActivationEvents() []types.EventType {
	return []types.EventType{types.QUERY_UNDERSTAND}
}

// OnEvent processes triggered events
// When receiving a QUERY_UNDERSTAND event, it extracts entities from the query
func (p *PluginExtractEntity) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	if strings.ToLower(os.Getenv("NEO4J_ENABLE")) != "true" {
		logger.Debugf(ctx, "skipping extract entity, neo4j is disabled")
		return next()
	}
	if !ShouldUseGraph(chatManage) {
		logger.Debugf(ctx, "skipping extract entity, entity relation not needed")
		return next()
	}

	query := chatManage.Query

	model, err := p.modelService.GetChatModel(ctx, chatManage.ChatModelID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get model, session_id: %s, error: %v", chatManage.SessionID, err)
		return next()
	}

	// Collect all knowledge base IDs to query
	kbIDSet := make(map[string]struct{})
	for _, id := range chatManage.KnowledgeBaseIDs {
		kbIDSet[id] = struct{}{}
	}

	// If KnowledgeIDs is specified, retrieve them and collect their knowledge base IDs (include shared KB docs)
	// Also build a mapping from KnowledgeID to KnowledgeBaseID
	knowledgeToKBMap := make(map[string]string)
	if len(chatManage.KnowledgeIDs) > 0 {
		knowledges, err := p.knowledgeService.GetKnowledgeBatchWithSharedAccess(ctx, chatManage.TenantID, chatManage.KnowledgeIDs)
		if err != nil {
			logger.Errorf(ctx, "failed to get knowledges: %v", err)
			return next()
		}
		for _, k := range knowledges {
			kbIDSet[k.KnowledgeBaseID] = struct{}{}
			knowledgeToKBMap[k.ID] = k.KnowledgeBaseID
		}
	}

	// Convert set to slice
	allKBIDs := make([]string, 0, len(kbIDSet))
	for id := range kbIDSet {
		allKBIDs = append(allKBIDs, id)
	}

	// Batch retrieve all knowledge bases
	kbs, err := p.knowledgeBaseRepo.GetKnowledgeBaseByIDs(ctx, allKBIDs)
	if err != nil {
		logger.Errorf(ctx, "failed to get knowledge bases: %v", err)
		return next()
	}

	// Check if any knowledge base has graph indexing enabled and collect their IDs
	enabledKBSet := make(map[string]struct{})
	for _, kb := range kbs {
		if kb.IsGraphEnabled() {
			enabledKBSet[kb.ID] = struct{}{}
		}
	}
	if len(enabledKBSet) == 0 {
		logger.Debugf(ctx, "no knowledge base has extract config enabled")
		return next()
	}

	// Save enabled knowledge base IDs for later use in search_entity
	enabledKBIDs := make([]string, 0, len(enabledKBSet))
	for id := range enabledKBSet {
		enabledKBIDs = append(enabledKBIDs, id)
	}
	chatManage.EntityKBIDs = enabledKBIDs

	// Filter knowledgeToKBMap to only include files from enabled knowledge bases
	entityKnowledge := make(map[string]string)
	for knowledgeID, kbID := range knowledgeToKBMap {
		if _, ok := enabledKBSet[kbID]; ok {
			entityKnowledge[knowledgeID] = kbID
		}
	}
	chatManage.EntityKnowledge = entityKnowledge

	template := &types.PromptTemplateStructured{
		Description: p.template.Description,
		Examples:    p.template.Examples,
	}
	extractor := NewExtractor(model, template)
	graph, err := extractor.Extract(ctx, query)
	if err != nil {
		logger.Errorf(ctx, "Failed to extract entities, session_id: %s, error: %v", chatManage.SessionID, err)
		return next()
	}
	nodes := []string{}
	for _, node := range graph.Node {
		nodes = append(nodes, node.Name)
	}
	logger.Debugf(ctx, "extracted node: %v", nodes)
	chatManage.Entity = nodes
	return next()
}

// ShouldUseGraph reports layer-1 graph search allowance (relation need ∧ routing budget).
// Open-graph KB availability is checked at the execution layer, not here.
func ShouldUseGraph(chatManage *types.ChatManage) bool {
	if chatManage == nil {
		return false
	}
	if chatManage.RoutingDecision != nil {
		return chatManage.RoutingDecision.Budget.GraphEnabled && chatManage.RoutingDecision.Classification.NeedsEntityRelation
	}
	return types.NeedsEntityRelation(chatManage.Query) || types.NeedsEntityRelation(chatManage.RewriteQuery)
}

// Extractor is a struct for extracting entities
type Extractor struct {
	chat     chat.Chat
	formater *Formater
	template *types.PromptTemplateStructured
	chatOpt  *chat.ChatOptions
}

const (
	defaultGraphMaxEntities   = types.DefaultGraphMaxEntities
	defaultGraphMaxRelations  = types.DefaultGraphMaxRelations
	defaultGraphMinConfidence = types.DefaultGraphMinConfidence
	maxGraphEntities          = types.MaxGraphEntities
	maxGraphRelations         = types.MaxGraphRelations
)

// NewExtractor creates a new extractor
func NewExtractor(
	chatModel chat.Chat,
	template *types.PromptTemplateStructured,
) Extractor {
	think := false
	return Extractor{
		chat:     chatModel,
		formater: NewFormater(),
		template: template,
		chatOpt: &chat.ChatOptions{
			Temperature: 0.3,
			MaxTokens:   4096,
			Thinking:    &think,
			Format:      graphExtractionFormatSchema(),
		},
	}
}

// Extract extracts entities from content
func (e *Extractor) Extract(ctx context.Context, content string) (*types.GraphData, error) {
	generator := NewQAPromptGenerator(e.formater, e.template)

	// logger.Debugf(ctx, "chat system: %s", generator.System(ctx))
	// logger.Debugf(ctx, "chat user: %s", generator.User(ctx, content))

	chatResponse, err := e.chat.Chat(ctx, generator.Render(ctx, content), e.chatOpt)
	if err != nil {
		logger.Errorf(ctx, "failed to chat: %v", err)
		return nil, err
	}

	graph, err := e.formater.ParseGraph(ctx, chatResponse.Content)
	if err != nil {
		logger.Errorf(ctx, "failed to parse graph: %v", err)
		return nil, err
	}
	e.ApplySchemaFilter(ctx, graph)
	e.ApplyExtractionPolicy(ctx, graph)
	return graph, nil
}

// ApplyExtractionPolicy enforces deterministic limits after schema filtering
// and before persistence.
func (e *Extractor) ApplyExtractionPolicy(ctx context.Context, graph *types.GraphData) {
	maxEntities := e.template.MaxEntities
	if maxEntities <= 0 {
		maxEntities = defaultGraphMaxEntities
	} else if maxEntities > maxGraphEntities {
		maxEntities = maxGraphEntities
	}
	maxRelations := e.template.MaxRelations
	if maxRelations <= 0 {
		maxRelations = defaultGraphMaxRelations
	} else if maxRelations > maxGraphRelations {
		maxRelations = maxGraphRelations
	}
	minConfidence := e.template.MinConfidence
	if minConfidence <= 0 || minConfidence > 1 {
		minConfidence = defaultGraphMinConfidence
	}
	ApplyGraphExtractionPolicy(ctx, graph, maxEntities, maxRelations, minConfidence)
}

// ApplyGraphExtractionPolicy caps graph growth and rejects relations whose
// endpoints are absent from the accepted entity set.
func ApplyGraphExtractionPolicy(ctx context.Context, graph *types.GraphData, maxEntities, maxRelations int, minConfidence float64) {
	if graph == nil {
		return
	}
	if len(graph.Node) > maxEntities {
		graph.Node = graph.Node[:maxEntities]
	}
	validNodes := make(map[string]struct{}, len(graph.Node))
	for _, node := range graph.Node {
		if node != nil {
			validNodes[node.Name] = struct{}{}
		}
	}
	relations := make([]*types.GraphRelation, 0, min(len(graph.Relation), maxRelations))
	for _, relation := range graph.Relation {
		if relation == nil || relation.Confidence < minConfidence {
			continue
		}
		if _, ok := validNodes[relation.Node1]; !ok {
			logger.Infof(ctx, "drop relation %s-%s: source entity was not extracted", relation.Node1, relation.Node2)
			continue
		}
		if _, ok := validNodes[relation.Node2]; !ok {
			logger.Infof(ctx, "drop relation %s-%s: target entity was not extracted", relation.Node1, relation.Node2)
			continue
		}
		relations = append(relations, relation)
		if len(relations) == maxRelations {
			break
		}
	}
	graph.Relation = relations
}

func graphExtractionFormatSchema() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{"items":{"type":"array","items":{"oneOf":[
    {"type":"object","additionalProperties":false,"properties":{"entity":{"type":"string","minLength":1},"entity_type":{"type":"string"},"entity_attributes":{"type":"array","items":{"type":"string"}},"aliases":{"type":"array","items":{"type":"string","minLength":1}}},"required":["entity"]},
    {"type":"object","additionalProperties":false,"properties":{"entity1":{"type":"string","minLength":1},"entity2":{"type":"string","minLength":1},"relation":{"type":"string","minLength":1},"confidence":{"type":"number","minimum":0,"maximum":1}},"required":["entity1","entity2","relation","confidence"]}
  ]}}},
  "required":["items"],
  "additionalProperties":false
}`)
}

// ApplySchemaFilter applies the unified schema filter using template options.
func (e *Extractor) ApplySchemaFilter(ctx context.Context, graph *types.GraphData) SchemaFilterResult {
	return ApplyGraphSchemaFilter(ctx, graph, SchemaFilterOptionsFromTemplate(e.template))
}

// RemoveUnknownRelation removes unknown relations from graph.
// Deprecated for direct use: prefer ApplySchemaFilter / ApplyGraphSchemaFilter so empty Tags
// no longer wipe all relations under non-strict mode.
func (e *Extractor) RemoveUnknownRelation(ctx context.Context, graph *types.GraphData) {
	e.ApplySchemaFilter(ctx, graph)
}

// QAPromptGenerator is a struct for generating QA prompts
type QAPromptGenerator struct {
	Formater        *Formater
	Template        *types.PromptTemplateStructured
	ExamplesHeading string
	QuestionHeading string
	QuestionPrefix  string
	AnswerPrefix    string
}

// NewQAPromptGenerator creates a new QA prompt generator
func NewQAPromptGenerator(formater *Formater, template *types.PromptTemplateStructured) *QAPromptGenerator {
	return &QAPromptGenerator{
		Formater:        formater,
		Template:        template,
		ExamplesHeading: "# Examples",
		QuestionHeading: "# Question",
		QuestionPrefix:  "Q: ",
		AnswerPrefix:    "A: ",
	}
}

// System generates a system prompt
func (qa *QAPromptGenerator) System(ctx context.Context) string {
	promptLines := []string{}

	if len(qa.Template.Tags) == 0 {
		promptLines = append(promptLines, qa.Template.Description)
	} else {
		tags, _ := json.Marshal(qa.Template.Tags)
		promptLines = append(promptLines, fmt.Sprintf(qa.Template.Description, string(tags)))
	}
	if len(qa.Template.EntityTypes) > 0 || qa.Template.StrictSchema {
		entityTypes, _ := json.Marshal(qa.Template.EntityTypes)
		promptLines = append(promptLines,
			"Entity types whitelist (entity_type must be chosen from this list when provided): "+string(entityTypes)+".",
			"Each entity object MUST include entity_type. Do not invent types outside the whitelist when it is non-empty.",
		)
	}
	maxEntities := qa.Template.MaxEntities
	if maxEntities <= 0 {
		maxEntities = defaultGraphMaxEntities
	} else if maxEntities > maxGraphEntities {
		maxEntities = maxGraphEntities
	}
	maxRelations := qa.Template.MaxRelations
	if maxRelations <= 0 {
		maxRelations = defaultGraphMaxRelations
	} else if maxRelations > maxGraphRelations {
		maxRelations = maxGraphRelations
	}
	minConfidence := qa.Template.MinConfidence
	if minConfidence <= 0 || minConfidence > 1 {
		minConfidence = defaultGraphMinConfidence
	}
	promptLines = append(promptLines,
		fmt.Sprintf("Return one JSON object with an items array. Extract at most %d entities and %d relationships.", maxEntities, maxRelations),
		fmt.Sprintf("Entity items use entity, entity_type, entity_attributes and aliases. Relationship items use entity1, entity2, relation and confidence (0-1); omit relationships below confidence %.2f. Relationship endpoints must exactly match extracted entity names.", minConfidence),
	)
	if len(qa.Template.Examples) > 0 {
		promptLines = append(promptLines, qa.ExamplesHeading)
		for _, example := range qa.Template.Examples {
			// Question
			promptLines = append(promptLines, fmt.Sprintf("%s%s", qa.QuestionPrefix, strings.TrimSpace(example.Text)))

			// Answer
			answer, err := qa.Formater.formatExtraction(example.Node, example.Relation)
			if err != nil {
				return ""
			}
			promptLines = append(promptLines, fmt.Sprintf("%s%s", qa.AnswerPrefix, answer))

			// new line
			promptLines = append(promptLines, "")
		}
	}
	return strings.Join(promptLines, "\n")
}

// User generates a user prompt
func (qa *QAPromptGenerator) User(ctx context.Context, question string) string {
	promptLines := []string{}
	promptLines = append(promptLines, qa.QuestionHeading)
	promptLines = append(promptLines, fmt.Sprintf("%s%s", qa.QuestionPrefix, question))
	promptLines = append(promptLines, qa.AnswerPrefix)
	return strings.Join(promptLines, "\n")
}

// Render renders a prompt
func (qa *QAPromptGenerator) Render(ctx context.Context, question string) []chat.Message {
	return []chat.Message{
		{
			Role:    "system",
			Content: qa.System(ctx),
		},
		{
			Role:    "user",
			Content: qa.User(ctx, question),
		},
	}
}

// FormatType is a type for format types
type FormatType string

const (
	// FormatTypeJSON is a format type for JSON
	FormatTypeJSON FormatType = "json"
	// FormatTypeYAML is a format type for YAML
	FormatTypeYAML FormatType = "yaml"
)

const (
	_FENCE_START   = "```"
	_LANGUAGE_TAG  = `(?P<lang>[A-Za-z0-9_+-]+)?`
	_FENCE_NEWLINE = `(?:\s*\n)?`
	_FENCE_BODY    = `(?P<body>[\s\S]*?)`
	_FENCE_END     = "```"
)

var _FENCE_RE = regexp.MustCompile(
	_FENCE_START + _LANGUAGE_TAG + _FENCE_NEWLINE + _FENCE_BODY + _FENCE_END,
)

// Formater is a struct for formatting entities
type Formater struct {
	attributeSuffix string
	formatType      FormatType
	useFences       bool
	nodePrefix      string

	relationSource string
	relationTarget string
	relationPrefix string
}

// NewFormater creates a new formater
func NewFormater() *Formater {
	return &Formater{
		attributeSuffix: "_attributes",
		formatType:      FormatTypeJSON,
		useFences:       true,
		nodePrefix:      "entity",
		relationSource:  "entity1",
		relationTarget:  "entity2",
		relationPrefix:  "relation",
	}
}

// formatExtraction formats extraction
func (f *Formater) formatExtraction(nodes []*types.GraphNode, relations []*types.GraphRelation) (string, error) {
	items := make([]map[string]interface{}, 0)
	for _, node := range nodes {
		if node == nil {
			continue
		}
		item := map[string]interface{}{
			f.nodePrefix: node.Name,
		}
		if strings.TrimSpace(node.EntityType) != "" {
			item[f.nodePrefix+"_type"] = node.EntityType
			item["entity_type"] = node.EntityType
		}
		if len(node.Attributes) > 0 {
			item[fmt.Sprintf("%s%s", f.nodePrefix, f.attributeSuffix)] = node.Attributes
		}
		if len(node.Aliases) > 0 {
			item["aliases"] = node.Aliases
		}
		items = append(items, item)
	}
	for _, relation := range relations {
		if relation == nil {
			continue
		}
		item := map[string]interface{}{
			f.relationSource: relation.Node1,
			f.relationTarget: relation.Node2,
			f.relationPrefix: relation.Type,
		}
		if relation.Confidence > 0 {
			item["confidence"] = relation.Confidence
		}
		items = append(items, item)
	}
	formatted := ""
	switch f.formatType {
	default:
		formattedBytes, err := json.MarshalIndent(map[string]interface{}{"items": items}, "", "  ")
		if err != nil {
			return "", err
		}
		formatted = string(formattedBytes)
	}
	if f.useFences {
		formatted = f.addFences(formatted)
	}
	return formatted, nil
}

func (f *Formater) parseOutput(ctx context.Context, text string) ([]map[string]interface{}, error) {
	if text == "" {
		return nil, errors.New("empty or invalid input string")
	}
	content := f.extractContent(ctx, text)
	// logger.Debugf(ctx, "Extracted content: %s", content)
	if content == "" {
		return nil, errors.New("empty or invalid input string")
	}

	var parsed interface{}
	var err error
	if f.formatType == FormatTypeJSON {
		err = json.Unmarshal([]byte(content), &parsed)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s content: %s", strings.ToUpper(string(f.formatType)), err.Error())
	}
	if parsed == nil {
		return nil, fmt.Errorf("content must be a list of extractions or a dict")
	}

	var items []interface{}
	if parsedMap, ok := parsed.(map[string]interface{}); ok {
		if nested, ok := parsedMap["items"].([]interface{}); ok {
			items = nested
		} else {
			items = []interface{}{parsedMap}
		}
	} else if parsedList, ok := parsed.([]interface{}); ok {
		items = parsedList
	} else {
		return nil, fmt.Errorf("expected list or dict, got %T", parsed)
	}

	itemsList := make([]map[string]interface{}, 0)
	for _, item := range items {
		if itemMap, ok := item.(map[string]interface{}); ok {
			itemsList = append(itemsList, itemMap)
		} else {
			return nil, fmt.Errorf("each item in the sequence must be a mapping.")
		}
	}
	return itemsList, nil
}

func (f *Formater) ParseGraph(ctx context.Context, text string) (*types.GraphData, error) {
	matchData, err := f.parseOutput(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(matchData) == 0 {
		logger.Debugf(ctx, "received empty extraction data.")
		return &types.GraphData{}, nil
	}
	// mm, _ := json.Marshal(matchData)
	// logger.Debugf(ctx, "Parsed graph data: %s", string(mm))

	var nodes []*types.GraphNode
	var relations []*types.GraphRelation

	for _, group := range matchData {
		switch {
		case group[f.nodePrefix] != nil:
			attributes := make([]string, 0)
			attributesKey := f.nodePrefix + f.attributeSuffix
			if attr, ok := group[attributesKey].([]interface{}); ok {
				for _, v := range attr {
					attributes = append(attributes, fmt.Sprintf("%v", v))
				}
			}
			entityType := ""
			for _, key := range []string{"entity_type", f.nodePrefix + "_type", "type"} {
				if v := group[key]; v != nil {
					entityType = strings.TrimSpace(fmt.Sprintf("%v", v))
					if entityType != "" {
						break
					}
				}
			}
			aliases := stringSliceFromGraphValue(group["aliases"])
			nodes = append(nodes, &types.GraphNode{
				Name:       fmt.Sprintf("%v", group[f.nodePrefix]),
				EntityType: entityType,
				Aliases:    aliases,
				Attributes: attributes,
			})
		case group[f.relationSource] != nil && group[f.relationTarget] != nil:
			relations = append(relations, &types.GraphRelation{
				Node1:      fmt.Sprintf("%v", group[f.relationSource]),
				Node2:      fmt.Sprintf("%v", group[f.relationTarget]),
				Type:       fmt.Sprintf("%v", group[f.relationPrefix]),
				Confidence: graphConfidence(group["confidence"]),
			})
		default:
			logger.Warnf(ctx, "Unsupported graph group: %v", group)
			continue
		}
	}
	graph := &types.GraphData{
		Node:     nodes,
		Relation: relations,
	}
	f.rebuildGraph(ctx, graph)
	return graph, nil
}

func stringSliceFromGraphValue(value interface{}) []string {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(fmt.Sprintf("%v", value))
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func graphConfidence(value interface{}) float64 {
	if value == nil {
		return defaultGraphMinConfidence
	}
	number, ok := value.(float64)
	if !ok {
		return 0
	}
	if number < 0 || number > 1 {
		return 0
	}
	return number
}

func (f *Formater) rebuildGraph(ctx context.Context, graph *types.GraphData) {
	nodeMap := make(map[string]*types.GraphNode)
	nodes := make([]*types.GraphNode, 0, len(graph.Node))
	for _, node := range graph.Node {
		if node == nil {
			continue
		}
		node.Name = strings.TrimSpace(node.Name)
		node.EntityType = strings.TrimSpace(node.EntityType)
		if node.Name == "" {
			logger.Infof(ctx, "Drop entity with empty name")
			continue
		}
		if prenode, ok := nodeMap[node.Name]; ok {
			logger.Infof(ctx, "Duplicate node ID: %s, merge attribute", node.Name)
			prenode.Attributes = mergeGraphStrings(prenode.Attributes, node.Attributes)
			prenode.Aliases = mergeGraphStrings(prenode.Aliases, node.Aliases)
			if prenode.EntityType == "" {
				prenode.EntityType = node.EntityType
			}
			continue
		}
		nodeMap[node.Name] = node
		nodes = append(nodes, node)
	}

	relations := make([]*types.GraphRelation, 0, len(graph.Relation))
	for _, relation := range graph.Relation {
		if relation == nil {
			continue
		}
		relation.Node1 = strings.TrimSpace(relation.Node1)
		relation.Node2 = strings.TrimSpace(relation.Node2)
		relation.Type = strings.TrimSpace(relation.Type)
		if relation.Node1 == "" || relation.Node2 == "" || relation.Type == "" {
			logger.Infof(ctx, "Drop relation with empty endpoint or type")
			continue
		}
		if relation.Node1 == relation.Node2 {
			logger.Infof(ctx, "Duplicate relation, ignore it")
			continue
		}

		if _, ok := nodeMap[relation.Node1]; !ok {
			logger.Infof(ctx, "Drop relation with unknown source node ID: %s", relation.Node1)
			continue
		}
		if _, ok := nodeMap[relation.Node2]; !ok {
			logger.Infof(ctx, "Drop relation with unknown target node ID: %s", relation.Node2)
			continue
		}

		relations = append(relations, relation)
	}
	*graph = types.GraphData{
		Node:     nodes,
		Relation: relations,
	}
}

func mergeGraphStrings(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	result := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (f *Formater) extractContent(ctx context.Context, text string) string {
	if !f.useFences {
		return strings.TrimSpace(text)
	}
	validTags := map[FormatType]map[string]struct{}{
		FormatTypeYAML: {"yaml": {}, "yml": {}},
		FormatTypeJSON: {"json": {}},
	}
	matches := _FENCE_RE.FindAllStringSubmatch(text, -1)
	var candidates []string
	for _, match := range matches {
		lang := match[1]
		body := match[2]
		if f.isValidLanguageTag(lang, validTags) {
			candidates = append(candidates, body)
		}
	}
	switch {
	case len(candidates) == 1:
		return strings.TrimSpace(candidates[0])

	case len(candidates) > 1:
		logger.Warnf(ctx, "multiple candidates found: %d", len(candidates))
		return strings.TrimSpace(candidates[0])

	case len(matches) == 1:
		logger.Debugf(ctx, "no candidate found, use first match without language tag: %s", matches[0][1])
		return strings.TrimSpace(matches[0][2])

	case len(matches) > 1:
		logger.Warnf(ctx, "multiple matches found: %d", len(matches))
		return strings.TrimSpace(matches[0][2])

	default:
		logger.Warnf(ctx, "no match found")
		return strings.TrimSpace(text)
	}
}

func (f *Formater) addFences(content string) string {
	content = strings.TrimSpace(content)
	return fmt.Sprintf("```%s\n%s\n```", f.formatType, content)
}

func (f *Formater) isValidLanguageTag(lang string, validTags map[FormatType]map[string]struct{}) bool {
	if lang == "" {
		return true
	}
	tag := strings.TrimSpace(strings.ToLower(lang))
	validSet, ok := validTags[f.formatType]
	if !ok {
		return false
	}
	_, exists := validSet[tag]
	return exists
}
