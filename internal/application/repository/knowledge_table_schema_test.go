package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKnowledgeTableSchemaIsTenantAndRevisionScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeTableSchema{}); err != nil {
		t.Fatal(err)
	}
	one := &types.Knowledge{ID: "table", TenantID: 1, FileType: "xlsx", FileHash: "hash-a"}
	two := &types.Knowledge{ID: "other", TenantID: 2, FileType: "xlsx", FileHash: "hash-b"}
	if err := db.Create(one).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(two).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewKnowledgeTableSchemaRepository(db)
	if err := repo.Upsert(context.Background(), one, []byte(`{"table_name":"table"}`)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.GetCurrent(context.Background(), one); err != nil || !found {
		t.Fatalf("expected current schema, found=%v err=%v", found, err)
	}
	if _, found, err := repo.GetCurrent(context.Background(), two); err != nil || found {
		t.Fatalf("schema leaked across tenant/document, found=%v err=%v", found, err)
	}
	one.FileHash = "hash-new"
	if _, found, err := repo.GetCurrent(context.Background(), one); err != nil || found {
		t.Fatalf("stale schema matched new revision, found=%v err=%v", found, err)
	}
}

func TestKnowledgeTableSchemaUpsertDropsSampleValues(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeTableSchema{}); err != nil {
		t.Fatal(err)
	}
	knowledge := &types.Knowledge{ID: "private", TenantID: 9, FileType: "xlsx", FileHash: "hash"}
	if err := db.Create(knowledge).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewKnowledgeTableSchemaRepository(db)
	payload := []byte(`{"table_name":"data","columns":[{"name":"姓名","type":"VARCHAR","value_examples":["张三"]}]}`)
	if err := repo.Upsert(context.Background(), knowledge, payload); err != nil {
		t.Fatal(err)
	}
	stored, found, err := repo.GetCurrent(context.Background(), knowledge)
	if err != nil || !found {
		t.Fatalf("schema not stored: found=%v err=%v", found, err)
	}
	var decoded struct {
		Columns []map[string]any `json:"columns"`
	}
	if err := json.Unmarshal(stored, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded.Columns[0]["value_examples"]; exists {
		t.Fatalf("sample values persisted: %s", stored)
	}
}

func TestKnowledgeTableSchemaDoesNotReappearAfterDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeTableSchema{}); err != nil {
		t.Fatal(err)
	}
	knowledge := &types.Knowledge{ID: "deleted", TenantID: 7, FileType: "csv", FileHash: "hash"}
	if err := db.Create(knowledge).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(knowledge).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewKnowledgeTableSchemaRepository(db)
	if err := repo.Upsert(context.Background(), knowledge, []byte(`{"table_name":"deleted"}`)); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&types.KnowledgeTableSchema{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted knowledge schema was recreated: %d", count)
	}
}
