package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestBuildGraphFewShotExamplesIsOptional(t *testing.T) {
	require.Nil(t, buildGraphFewShotExamples(nil))
	require.Nil(t, buildGraphFewShotExamples(&types.ExtractConfig{}))
	require.Nil(t, buildGraphFewShotExamples(&types.ExtractConfig{
		Text: "only text",
	}))
	require.Nil(t, buildGraphFewShotExamples(&types.ExtractConfig{
		Nodes: []*types.GraphNode{{Name: "only output", EntityType: "concept"}},
	}))
	require.Nil(t, buildGraphFewShotExamples(&types.ExtractConfig{
		Text:  "legacy example without entity type",
		Nodes: []*types.GraphNode{{Name: "legacy"}},
	}))
	require.Nil(t, buildGraphFewShotExamples(&types.ExtractConfig{
		Text:      "relation endpoint is missing",
		Nodes:     []*types.GraphNode{{Name: "A", EntityType: "concept"}},
		Relations: []*types.GraphRelation{{Node1: "A", Node2: "B", Type: "related_to"}},
	}))
}

func TestBuildGraphFewShotExamplesIncludesCompleteExample(t *testing.T) {
	config := &types.ExtractConfig{
		Text: "订单服务使用 JMeter 进行测试",
		Nodes: []*types.GraphNode{
			{Name: "订单服务", EntityType: "assessment_object"},
			{Name: "测试", EntityType: "test_method"},
		},
		Relations: []*types.GraphRelation{{Node1: "测试", Node2: "订单服务", Type: "tests"}},
	}

	examples := buildGraphFewShotExamples(config)

	require.Len(t, examples, 1)
	require.Equal(t, config.Text, examples[0].Text)
	require.Same(t, config.Nodes[0], examples[0].Node[0])
}
