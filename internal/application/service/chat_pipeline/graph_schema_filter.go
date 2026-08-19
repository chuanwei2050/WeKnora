package chatpipeline

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// SchemaFilterOptions controls graph schema filtering for extract paths.
type SchemaFilterOptions struct {
	Tags         []string
	EntityTypes  []string
	StrictSchema bool
}

// SchemaFilterResult reports whether the filtered graph should skip persistence.
type SchemaFilterResult struct {
	SkipWrite bool
	Reason    string
}

// SchemaFilterOptionsFromExtract builds options from knowledge-base extract config.
func SchemaFilterOptionsFromExtract(cfg *types.ExtractConfig) SchemaFilterOptions {
	if cfg == nil {
		return SchemaFilterOptions{}
	}
	return SchemaFilterOptions{
		Tags:         append([]string(nil), cfg.Tags...),
		EntityTypes:  append([]string(nil), cfg.EntityTypes...),
		StrictSchema: cfg.StrictSchema,
	}
}

// SchemaFilterOptionsFromTemplate builds options from prompt template fields.
func SchemaFilterOptionsFromTemplate(tpl *types.PromptTemplateStructured) SchemaFilterOptions {
	if tpl == nil {
		return SchemaFilterOptions{}
	}
	return SchemaFilterOptions{
		Tags:         append([]string(nil), tpl.Tags...),
		EntityTypes:  append([]string(nil), tpl.EntityTypes...),
		StrictSchema: tpl.StrictSchema,
	}
}

// ApplyGraphSchemaFilter mutates graph in place according to whitelist rules.
func ApplyGraphSchemaFilter(ctx context.Context, graph *types.GraphData, opts SchemaFilterOptions) SchemaFilterResult {
	if graph == nil {
		return SchemaFilterResult{SkipWrite: true, Reason: "nil_graph"}
	}

	if opts.StrictSchema && (len(opts.Tags) == 0 || len(opts.EntityTypes) == 0) {
		logger.Infof(ctx, "strict schema skip write: tags=%d entity_types=%d", len(opts.Tags), len(opts.EntityTypes))
		graph.Relation = nil
		if len(opts.EntityTypes) == 0 {
			graph.Node = nil
		}
		return SchemaFilterResult{SkipWrite: true, Reason: "strict_incomplete_whitelist"}
	}

	if len(opts.Tags) > 0 {
		allowed := make(map[string]string, len(opts.Tags))
		for _, tag := range opts.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			allowed[strings.ToLower(tag)] = tag
		}
		kept := make([]*types.GraphRelation, 0, len(graph.Relation))
		for _, relation := range graph.Relation {
			if relation == nil {
				continue
			}
			if canonical, ok := allowed[strings.ToLower(strings.TrimSpace(relation.Type))]; ok {
				relation.Type = canonical
				kept = append(kept, relation)
				continue
			}
			logger.Infof(ctx, "Unknown relation type %s with %v, ignore it", relation.Type, opts.Tags)
		}
		graph.Relation = kept
	}

	if opts.StrictSchema && len(opts.EntityTypes) > 0 {
		allowedTypes := make(map[string]string, len(opts.EntityTypes))
		for _, entityType := range opts.EntityTypes {
			entityType = strings.TrimSpace(entityType)
			if entityType == "" {
				continue
			}
			allowedTypes[strings.ToLower(entityType)] = entityType
		}
		validNodes := make(map[string]struct{})
		keptNodes := make([]*types.GraphNode, 0, len(graph.Node))
		for _, node := range graph.Node {
			if node == nil {
				continue
			}
			entityType := strings.TrimSpace(node.EntityType)
			if entityType == "" {
				logger.Infof(ctx, "strict schema drop node %s: empty entity type", node.Name)
				continue
			}
			canonicalType, ok := allowedTypes[strings.ToLower(entityType)]
			if !ok {
				logger.Infof(ctx, "strict schema drop node %s: unknown entity type %s", node.Name, entityType)
				continue
			}
			node.EntityType = canonicalType
			keptNodes = append(keptNodes, node)
			validNodes[node.Name] = struct{}{}
		}
		graph.Node = keptNodes

		keptRelations := make([]*types.GraphRelation, 0, len(graph.Relation))
		for _, relation := range graph.Relation {
			if relation == nil {
				continue
			}
			if _, ok := validNodes[relation.Node1]; !ok {
				logger.Infof(ctx, "strict schema drop relation %s-%s: invalid source entity", relation.Node1, relation.Node2)
				continue
			}
			if _, ok := validNodes[relation.Node2]; !ok {
				logger.Infof(ctx, "strict schema drop relation %s-%s: invalid target entity", relation.Node1, relation.Node2)
				continue
			}
			keptRelations = append(keptRelations, relation)
		}
		graph.Relation = keptRelations
	}

	if len(graph.Relation) == 0 {
		return SchemaFilterResult{SkipWrite: true, Reason: "no_valid_relations"}
	}
	return SchemaFilterResult{}
}
