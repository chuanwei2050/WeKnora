package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestExtractionRequestsAllowDefaultModel(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		target  any
	}{
		{
			name:    "text relation extraction",
			payload: `{"text":"example","model_id":""}`,
			target:  &TextRelationExtractionRequest{},
		},
		{
			name:    "example text generation",
			payload: `{"tags":[],"model_id":""}`,
			target:  &FabriTextRequest{},
		},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.payload))
			request.Header.Set("Content-Type", "application/json")
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = request

			if err := context.ShouldBindJSON(tt.target); err != nil {
				t.Fatalf("bind request with default model: %v", err)
			}
		})
	}
}

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
			"entity_schema": [{"type":"tool","base_type":"RESOURCE","description":"测试工具"}],
			"relation_schema": [{"type":"uses","source_type":"tool","target_type":"tool","description":"工具使用工具"}],
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
	if len(config.EntitySchema) != 1 || config.EntitySchema[0].BaseType != "RESOURCE" || config.EntitySchema[0].Description != "测试工具" || len(config.RelationSchema) != 1 || config.RelationSchema[0].SourceType != "tool" {
		t.Fatalf("structured schema lost: %#v %#v", config.EntitySchema, config.RelationSchema)
	}
	if len(config.Nodes) != 1 || len(config.Nodes[0].Aliases) != 1 || config.Nodes[0].Aliases[0] != "Alpha" {
		t.Fatalf("node aliases lost: %#v", config.Nodes)
	}
	if len(config.Relations) != 1 || config.Relations[0].Confidence != 0.8 {
		t.Fatalf("relation confidence lost: %#v", config.Relations)
	}
}

func TestGraphExtractConfigRequestPreservesSchemaWhenDisabled(t *testing.T) {
	var req KBModelConfigRequest
	payload := []byte(`{
		"nodeExtract": {
			"enabled": false,
			"mode": "template",
			"template_key": "software-testing",
			"tags": ["uses"],
			"entity_types": ["tool"],
			"entity_schema": [{"type":"tool","base_type":"RESOURCE","description":"测试工具"}],
			"relation_schema": [{"type":"uses","source_type":"tool","target_type":"tool","description":"工具使用工具"}],
			"strict_schema": true,
			"require_triple_review": true
		}
	}`)
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	config := req.NodeExtract.extractConfig()
	if err := validateExtractConfig(config); err != nil {
		t.Fatalf("disabled graph config should be valid: %v", err)
	}
	if config.Enabled || config.TemplateKey != "software-testing" || len(config.EntitySchema) != 1 || len(config.RelationSchema) != 1 {
		t.Fatalf("disabled graph config lost its schema: %#v", config)
	}
	if !config.StrictSchema || !config.RequireTripleReview {
		t.Fatalf("disabled graph config lost its policies: %#v", config)
	}
}

func TestValidateExtractConfigRejectsInvalidStructuredRelationDirection(t *testing.T) {
	config := &types.ExtractConfig{
		Enabled: true,
		Mode:    types.GraphExtractionCustom,
		EntitySchema: []types.GraphEntityTypeDefinition{
			{Type: "tool", BaseType: "RESOURCE", Description: "工具"},
		},
		RelationSchema: []types.GraphRelationTypeDefinition{
			{Type: "uses", SourceType: "method", TargetType: "tool", Description: "使用"},
		},
	}
	if err := validateExtractConfig(config); err == nil {
		t.Fatal("unknown relation source type must be rejected")
	}
}

func TestValidateExtractConfigDefaultsPolicyAndAllowsNoExamples(t *testing.T) {
	config := &types.ExtractConfig{Enabled: true, Mode: types.GraphExtractionGeneral}
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

func TestValidateExtractConfigRejectsPartialFewShot(t *testing.T) {
	config := &types.ExtractConfig{Enabled: true, Mode: types.GraphExtractionGeneral, Text: "example"}
	if err := validateExtractConfig(config); err == nil {
		t.Fatal("partial few-shot must be rejected")
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
