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
	disabled := &types.KnowledgeBase{
		IndexingStrategy: types.IndexingStrategy{GraphEnabled: false},
		ExtractConfig:    &types.ExtractConfig{Enabled: true},
	}

	if ShouldEnqueueGraphExtract(disabled, "A 和 B 是什么关系？") {
		t.Fatal("disabled graph must not enqueue")
	}
	if ShouldEnqueueGraphExtract(enabled, "什么是向量数据库？") {
		t.Fatal("non-relation chunk must not enqueue")
	}
	if !ShouldEnqueueGraphExtract(enabled, "A 和 B 是什么关系？") {
		t.Fatal("relation-bearing chunk on graph-enabled KB must enqueue")
	}
	if ShouldEnqueueGraphExtract(nil, "A 和 B 是什么关系？") {
		t.Fatal("nil kb must not enqueue")
	}
}
