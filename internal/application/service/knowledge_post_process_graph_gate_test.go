package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestShouldEnqueueGraphExtract(t *testing.T) {
	enabled := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{GraphEnabled: true},
		ExtractConfig:    &types.ExtractConfig{Enabled: true},
	}
	signalOnly := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{GraphEnabled: true},
		ExtractConfig:    &types.ExtractConfig{Enabled: true, IngestionMode: types.GraphIngestionSignal},
	}
	disabled := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{GraphEnabled: false},
		ExtractConfig:    &types.ExtractConfig{Enabled: true},
	}

	if ShouldEnqueueGraphExtract(disabled, "A 和 B 是什么关系？") {
		t.Fatal("disabled graph must not enqueue")
	}
	if !ShouldEnqueueGraphExtract(enabled, "张三任职于腾讯。") {
		t.Fatal("default all mode must enqueue non-empty facts")
	}
	if !ShouldEnqueueGraphExtract(enabled, "A 和 B 是什么关系？") {
		t.Fatal("relation-bearing chunk on graph-enabled KB must enqueue")
	}
	if ShouldEnqueueGraphExtract(signalOnly, "张三任职于腾讯。") {
		t.Fatal("signal mode must preserve the explicit signal gate")
	}
	if !ShouldEnqueueGraphExtract(signalOnly, "A 和 B 是什么关系？") {
		t.Fatal("signal mode must enqueue relation-bearing chunks")
	}
	if ShouldEnqueueGraphExtract(enabled, "  ") {
		t.Fatal("empty chunks must not enqueue")
	}
	if ShouldEnqueueGraphExtract(nil, "A 和 B 是什么关系？") {
		t.Fatal("nil kb must not enqueue")
	}
}

func TestGraphExtractionModelID(t *testing.T) {
	kb := &types.KnowledgeBase{SummaryModelID: "summary", ExtractConfig: &types.ExtractConfig{ModelID: " graph "}}
	if got := kb.GraphExtractionModelID(); got != "graph" {
		t.Fatalf("expected dedicated graph model, got %q", got)
	}
	kb.ExtractConfig.ModelID = ""
	if got := kb.GraphExtractionModelID(); got != "summary" {
		t.Fatalf("expected summary fallback, got %q", got)
	}
}
