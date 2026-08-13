package types

import "strings"

// NeedsEntityRelation reports whether text contains an explicit signal that
// entity extraction and relation traversal may add value. It is the
// deterministic fallback for requests without a routing decision and the
// pre-filter for graph extraction during document ingestion.
func NeedsEntityRelation(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}

	for _, signal := range graphRelationSignals {
		if strings.Contains(value, signal) {
			return true
		}
	}
	return false
}

var graphRelationSignals = []string{
	// Relation, hierarchy and multi-hop expressions in Chinese.
	"关系", "关联", "联系", "连接", "属于", "包含", "组成", "构成", "依赖",
	"上下游", "上游", "下游", "层级", "分类", "类别", "从属", "配料", "成分",
	"适合人群", "链路", "路径", "多跳", "跨文档", "之间", "作者", "创始人",
	"负责人", "父亲", "母亲", "儿子", "女儿", "产地", "由什么组成",
	// Relation, hierarchy and multi-hop expressions in English.
	"relationship", "related to", "connected", "belongs to", "contains", "includes",
	"composed of", "consists of", "depends on", "upstream", "downstream", "hierarchy",
	"taxonomy", "category", "path", "multi-hop", "multihop", "cross-document", "author of",
	"written by", "known as", "part of", "member of", "used for", "suitable for", "made of",
}
