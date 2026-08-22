package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type bootstrapUserRepository struct {
	interfaces.UserRepository
	users       []*types.User
	updateCalls int
}

func (r *bootstrapUserRepository) ListUsers(context.Context, int, int) ([]*types.User, error) {
	return r.users, nil
}

func (r *bootstrapUserRepository) UpdateUser(context.Context, *types.User) error {
	r.updateCalls++
	return nil
}

func TestEnsureDefaultAdminDoesNotElevateExistingUser(t *testing.T) {
	t.Setenv("DEFAULT_ADMIN_ENABLED", "true")

	existing := &types.User{
		Username: "admin",
		Role:     types.UserRoleMember,
	}
	repo := &bootstrapUserRepository{users: []*types.User{existing}}
	service := &userService{userRepo: repo}

	if err := service.EnsureDefaultAdmin(context.Background()); err != nil {
		t.Fatalf("EnsureDefaultAdmin() error = %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("UpdateUser() calls = %d, want 0", repo.updateCalls)
	}
	if existing.Role != types.UserRoleMember {
		t.Fatalf("existing user role = %q, want %q", existing.Role, types.UserRoleMember)
	}
}
