package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openDisabledAgentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&types.TenantDisabledSharedAgent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestListDisabledOwnAgentIDsIncludesGlobalPlatformStatus(t *testing.T) {
	db := openDisabledAgentTestDB(t)
	repo := NewTenantDisabledSharedAgentRepository(db)
	ctx := context.Background()

	if err := repo.Add(ctx, types.PlatformAgentTenantID, "platform-agent", types.PlatformAgentTenantID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Add(ctx, 101, "tenant-agent", 101); err != nil {
		t.Fatal(err)
	}

	for _, tenantID := range []uint64{101, 202} {
		ids, err := repo.ListDisabledOwnAgentIDs(ctx, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		if !containsAgentID(ids, "platform-agent") {
			t.Fatalf("tenant %d did not inherit global platform status: %v", tenantID, ids)
		}
	}

	otherTenantIDs, err := repo.ListDisabledOwnAgentIDs(ctx, 202)
	if err != nil {
		t.Fatal(err)
	}
	if containsAgentID(otherTenantIDs, "tenant-agent") {
		t.Fatalf("tenant-scoped status leaked to another tenant: %v", otherTenantIDs)
	}
}

func containsAgentID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
