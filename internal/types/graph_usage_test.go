package types

import "testing"

func TestNeedsEntityRelation(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		needs bool
	}{
		{name: "ordinary fact", text: "向量数据库是什么？", needs: false},
		{name: "relationship", text: "A 和 B 是什么关系？", needs: true},
		{name: "hierarchy", text: "这个工具属于哪一类？", needs: true},
		{name: "multi hop", text: "查询从 A 到 C 的多跳路径", needs: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsEntityRelation(tt.text); got != tt.needs {
				t.Fatalf("NeedsEntityRelation(%q) = %v, want %v", tt.text, got, tt.needs)
			}
		})
	}
}
