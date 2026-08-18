package types

import "testing"

func TestEffectiveRole(t *testing.T) {
	tests := []struct {
		name string
		user *User
		want UserRole
	}{
		{name: "nil user", user: nil, want: UserRoleMember},
		{name: "explicit platform admin", user: &User{Role: UserRolePlatformAdmin}, want: UserRolePlatformAdmin},
		{name: "explicit tenant admin", user: &User{Role: UserRoleTenantAdmin}, want: UserRoleTenantAdmin},
		{name: "explicit member", user: &User{Role: UserRoleMember}, want: UserRoleMember},
		{name: "legacy cross tenant flag", user: &User{Role: UserRoleMember, CanAccessAllTenants: true}, want: UserRolePlatformAdmin},
		{name: "legacy native user", user: &User{}, want: UserRoleTenantAdmin},
		{name: "legacy sso tenant admin", user: &User{Role: UserRoleMember, BidReviewRole: "tenant_admin"}, want: UserRoleTenantAdmin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.user.EffectiveRole(); got != test.want {
				t.Fatalf("EffectiveRole() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUserCanAccessKnowledgeBase(t *testing.T) {
	tests := []struct {
		name   string
		user   *User
		kbID   string
		access bool
	}{
		{name: "legacy member defaults to all", user: &User{Role: UserRoleMember}, kbID: "kb-a", access: true},
		{name: "selected member can access selected KB", user: &User{Role: UserRoleMember, KnowledgeBaseAccessMode: KnowledgeBaseAccessSelected, KnowledgeBaseIDs: StringArray{"kb-a"}}, kbID: "kb-a", access: true},
		{name: "selected member cannot access other KB", user: &User{Role: UserRoleMember, KnowledgeBaseAccessMode: KnowledgeBaseAccessSelected, KnowledgeBaseIDs: StringArray{"kb-a"}}, kbID: "kb-b", access: false},
		{name: "tenant admin always accesses all", user: &User{Role: UserRoleTenantAdmin, KnowledgeBaseAccessMode: KnowledgeBaseAccessSelected}, kbID: "kb-b", access: true},
		{name: "empty KB ID is rejected", user: &User{Role: UserRoleMember}, kbID: "", access: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.CanAccessKnowledgeBase(tt.kbID); got != tt.access {
				t.Fatalf("CanAccessKnowledgeBase() = %v, want %v", got, tt.access)
			}
		})
	}
}
