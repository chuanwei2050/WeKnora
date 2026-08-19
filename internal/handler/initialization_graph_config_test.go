package handler

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestGraphExtractConfigRequestPreservesPolicyFields(t *testing.T) {
	var req KBModelConfigRequest
	payload := []byte(`{
		"nodeExtract": {
			"enabled": true,
			"model_id": "graph-model",
			"ingestion_mode": "signal",
			"max_entities": 20,
			"max_relations": 30,
			"min_confidence": 0.7,
			"text": "example",
			"tags": ["uses"],
			"entity_types": ["tool"],
			"strict_schema": true,
			"require_triple_review": true,
			"nodes": [{"name":"A","entity_type":"tool","aliases":["Alpha"]}],
			"relations": [{"node1":"A","node2":"A","type":"uses","confidence":0.8}]
		}
	}`)
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	config := req.NodeExtract.extractConfig()
	if config.ModelID != "graph-model" || config.IngestionMode != types.GraphIngestionSignal {
		t.Fatalf("model or ingestion mode lost: %#v", config)
	}
	if config.MaxEntities != 20 || config.MaxRelations != 30 || config.MinConfidence != 0.7 {
		t.Fatalf("extraction policy lost: %#v", config)
	}
	if !config.StrictSchema || !config.RequireTripleReview || len(config.EntityTypes) != 1 {
		t.Fatalf("schema or review policy lost: %#v", config)
	}
	if len(config.Nodes) != 1 || len(config.Nodes[0].Aliases) != 1 || config.Nodes[0].Aliases[0] != "Alpha" {
		t.Fatalf("node aliases lost: %#v", config.Nodes)
	}
	if len(config.Relations) != 1 || config.Relations[0].Confidence != 0.8 {
		t.Fatalf("relation confidence lost: %#v", config.Relations)
	}
}

func TestValidateExtractConfigDefaultsPolicyAndAllowsNoExamples(t *testing.T) {
	config := &types.ExtractConfig{Enabled: true, Text: "example"}
	if err := validateExtractConfig(config); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if config.IngestionMode != types.GraphIngestionAll ||
		config.MaxEntities != types.DefaultGraphMaxEntities ||
		config.MaxRelations != types.DefaultGraphMaxRelations ||
		config.MinConfidence != types.DefaultGraphMinConfidence {
		t.Fatalf("policy defaults not applied: %#v", config)
	}
}

func TestValidateGraphExtractionPolicyRejectsInvalidValues(t *testing.T) {
	tests := []types.ExtractConfig{
		{IngestionMode: "invalid"},
		{MaxEntities: types.MaxGraphEntities + 1},
		{MaxRelations: types.MaxGraphRelations + 1},
		{MinConfidence: 1.1},
	}
	for _, config := range tests {
		config := config
		if err := validateGraphExtractionPolicy(&config); err == nil {
			t.Fatalf("expected invalid policy to fail: %#v", config)
		}
	}
}

func TestValidateExtractConfigRejectsNullExamples(t *testing.T) {
	for _, config := range []*types.ExtractConfig{
		{Enabled: true, Text: "example", Nodes: []*types.GraphNode{nil}},
		{Enabled: true, Text: "example", Relations: []*types.GraphRelation{nil}},
	} {
		if err := validateExtractConfig(config); err == nil {
			t.Fatalf("expected null example to fail: %#v", config)
		}
	}
}
