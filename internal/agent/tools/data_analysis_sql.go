package tools

import (
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// bindAnalysisSQL changes identifiers, never string literals or comments.
func bindAnalysisSQL(query string, schema *TableSchema, knowledgeID string) (string, []string, error) {
	tree, err := pg_query.Parse(query)
	if err != nil {
		return "", nil, err
	}
	if len(tree.Stmts) != 1 || tree.Stmts[0].Stmt.GetSelectStmt() == nil {
		return "", nil, fmt.Errorf("one SELECT statement is required")
	}
	canonical := make(map[string]string)
	exact := make(map[string]bool)
	for _, column := range schema.Columns {
		exact[column.Name] = true
		key := normalizeIdentifierForMatch(column.Name)
		if old, exists := canonical[key]; exists && old != column.Name {
			canonical[key] = ""
		} else if !exists {
			canonical[key] = column.Name
		}
	}
	outputAliases := make(map[string]bool)
	for _, target := range tree.Stmts[0].Stmt.GetSelectStmt().TargetList {
		if result := target.GetResTarget(); result != nil && result.Name != "" {
			outputAliases[result.Name] = true
		}
	}
	var fixes []string
	relations := 0
	var bindErr error
	walkAnalysisSQL(tree.ProtoReflect(), func(message protoreflect.Message) {
		switch node := message.Interface().(type) {
		case *pg_query.RangeVar:
			relations++
			if node.Schemaname != "" || (node.Relname != "data" && node.Relname != knowledgeID && node.Relname != schema.TableName) {
				bindErr = fmt.Errorf("table %q is not the selected dataset; use data", node.Relname)
				return
			}
			// Keep qualified column references valid when a legacy physical name is used.
			if node.Alias == nil && node.Relname != schema.TableName {
				node.Alias = &pg_query.Alias{Aliasname: node.Relname}
			}
			node.Relname = schema.TableName
		case *pg_query.ColumnRef:
			if len(node.Fields) == 0 {
				return
			}
			field := node.Fields[len(node.Fields)-1].GetString_()
			if field == nil || exact[field.Sval] || (len(node.Fields) == 1 && outputAliases[field.Sval]) {
				return
			}
			if replacement := canonical[normalizeIdentifierForMatch(field.Sval)]; replacement != "" && replacement != field.Sval {
				fixes = append(fixes, fmt.Sprintf("%q -> %q", field.Sval, replacement))
				field.Sval = replacement
			}
		}
	})
	if bindErr != nil {
		return "", nil, bindErr
	}
	if relations == 0 {
		return "", nil, fmt.Errorf("analysis requires a reference to the selected dataset")
	}
	rewritten, err := pg_query.Deparse(tree)
	return strings.TrimSpace(rewritten), fixes, err
}

func walkAnalysisSQL(message protoreflect.Message, visit func(protoreflect.Message)) {
	visit(message)
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() != protoreflect.MessageKind {
			return true
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				walkAnalysisSQL(list.Get(i).Message(), visit)
			}
		} else {
			walkAnalysisSQL(value.Message(), visit)
		}
		return true
	})
}
