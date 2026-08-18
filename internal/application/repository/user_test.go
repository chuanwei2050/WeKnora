package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateUserPreservesNativeEmptyBidReviewRole(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}))

	repo := NewUserRepository(db)
	user := &types.User{
		ID:           "native-user",
		Username:     "native",
		Email:        "native@example.com",
		PasswordHash: "hash",
		IsActive:     true,
	}
	require.NoError(t, repo.CreateUser(context.Background(), user))

	stored, err := repo.GetUserByID(context.Background(), user.ID)
	require.NoError(t, err)
	require.Empty(t, stored.BidReviewRole)
	require.Empty(t, user.BidReviewRole)
	require.Equal(t, types.KnowledgeBaseAccessAll, stored.KnowledgeBaseAccessMode)
	require.Empty(t, stored.KnowledgeBaseIDs)
}

func TestHasUserDocumentActivity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.Knowledge{}, &types.KnowledgeVersion{}, &types.KnowledgeVersionReview{}))

	repo := NewUserRepository(db)
	active, err := repo.HasUserDocumentActivity(context.Background(), "member-a")
	require.NoError(t, err)
	require.False(t, active)

	require.NoError(t, db.Create(&types.KnowledgeVersionReview{ID: "review-a", VersionID: "version-a", ReviewerID: "member-a"}).Error)
	active, err = repo.HasUserDocumentActivity(context.Background(), "member-a")
	require.NoError(t, err)
	require.True(t, active)
}

func TestHasUserDocumentActivitySkipsUnavailableActivityTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}))

	repo := NewUserRepository(db)
	active, err := repo.HasUserDocumentActivity(context.Background(), "member-a")
	require.NoError(t, err)
	require.False(t, active)
}
