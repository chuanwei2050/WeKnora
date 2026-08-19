package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestValidateManagedPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "empty optional password", password: "", wantErr: false},
		{name: "valid password", password: "Admin@123456", wantErr: false},
		{name: "space", password: "Admin 123456", wantErr: true},
		{name: "Chinese", password: "Admin密码123", wantErr: true},
		{name: "missing letter", password: "12345678!", wantErr: true},
		{name: "missing number", password: "Password!", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateManagedPassword(tt.password)
			require.Equal(t, tt.wantErr, err != nil)
		})
	}
}

func TestValidateManagedUsername(t *testing.T) {
	require.NoError(t, validateManagedUsername("member-01"))
	require.Error(t, validateManagedUsername("  "))
	require.Error(t, validateManagedUsername("成员01"))
	require.Error(t, validateManagedUsername("-member"))
}

func TestValidateManagedNickname(t *testing.T) {
	require.NoError(t, validateManagedNickname("管理员"))
	require.Error(t, validateManagedNickname(""))
	require.Error(t, validateManagedNickname(string(make([]rune, 101))))
}

func TestValidateManagedRoleChange(t *testing.T) {
	require.NoError(t, validateManagedRoleChange(types.UserRoleMember, types.UserRoleTenantAdmin))
	require.NoError(t, validateManagedRoleChange(types.UserRoleTenantAdmin, types.UserRoleTenantAdmin))
	require.Error(t, validateManagedRoleChange(types.UserRoleTenantAdmin, types.UserRoleMember))
}

func TestNormalizeKnowledgeBaseIDs(t *testing.T) {
	ids, err := normalizeKnowledgeBaseIDs([]string{" kb-a ", "kb-a", "kb-b"})
	require.NoError(t, err)
	require.Equal(t, []string{"kb-a", "kb-b"}, ids)

	_, err = normalizeKnowledgeBaseIDs([]string{""})
	require.Error(t, err)
}
