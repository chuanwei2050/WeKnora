package types

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

type UserRole string

type KnowledgeBaseAccessMode string

const (
	UserRolePlatformAdmin UserRole = "platform_admin"
	UserRoleTenantAdmin   UserRole = "tenant_admin"
	UserRoleMember        UserRole = "member"

	KnowledgeBaseAccessAll      KnowledgeBaseAccessMode = "all"
	KnowledgeBaseAccessSelected KnowledgeBaseAccessMode = "selected"
)

func IsTenantUserRole(role UserRole) bool {
	return role == UserRoleTenantAdmin || role == UserRoleMember
}

func IsKnowledgeBaseAccessMode(mode KnowledgeBaseAccessMode) bool {
	return mode == KnowledgeBaseAccessAll || mode == KnowledgeBaseAccessSelected
}

type CreateTenantUserRequest struct {
	Username                string                  `json:"username" binding:"required,min=2,max=100"`
	Nickname                string                  `json:"nickname" binding:"omitempty,max=100"`
	Password                string                  `json:"password" binding:"required,min=8,max=72"`
	Role                    UserRole                `json:"role" binding:"required"`
	KnowledgeBaseAccessMode KnowledgeBaseAccessMode `json:"knowledge_base_access_mode"`
	KnowledgeBaseIDs        []string                `json:"knowledge_base_ids"`
}

type ResetTenantUserPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type UpdateTenantUserRequest struct {
	Username                string                  `json:"username" binding:"required,min=2,max=100"`
	Nickname                string                  `json:"nickname" binding:"omitempty,max=100"`
	Password                string                  `json:"password" binding:"omitempty,min=8,max=72"`
	Role                    UserRole                `json:"role" binding:"required"`
	KnowledgeBaseAccessMode KnowledgeBaseAccessMode `json:"knowledge_base_access_mode" binding:"required"`
	KnowledgeBaseIDs        []string                `json:"knowledge_base_ids"`
}

type UpdateTenantUserRoleRequest struct {
	Role UserRole `json:"role" binding:"required"`
}

type UpdateTenantUserStatusRequest struct {
	IsActive *bool `json:"is_active" binding:"required"`
}

// User represents a user in the system
type User struct {
	// Unique identifier of the user
	ID string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	// Username of the user
	Username string `json:"username"   gorm:"type:varchar(100);uniqueIndex;not null"`
	// Nickname displayed in the user interface
	Nickname string `json:"nickname" gorm:"type:varchar(100);not null;default:''"`
	// Email address of the user
	Email string `json:"email"      gorm:"type:varchar(255);uniqueIndex;not null"`
	// Hashed password of the user
	PasswordHash string `json:"-"          gorm:"type:varchar(255);not null"`
	// Avatar URL of the user
	Avatar string `json:"avatar"     gorm:"type:varchar(500)"`
	// Tenant ID that the user belongs to
	TenantID uint64 `json:"tenant_id"  gorm:"index"`
	// Whether the user is active
	IsActive bool `json:"is_active"  gorm:"default:true"`
	// Whether the user can access all tenants (cross-tenant access)
	CanAccessAllTenants bool `json:"can_access_all_tenants" gorm:"default:false"`
	// Role controls platform-wide and tenant-scoped management permissions.
	Role UserRole `json:"role" gorm:"type:varchar(32);not null;default:'member';index"`
	// KnowledgeBaseAccessMode controls whether a member sees all or selected tenant knowledge bases.
	KnowledgeBaseAccessMode KnowledgeBaseAccessMode `json:"knowledge_base_access_mode" gorm:"column:knowledge_base_access_mode;type:varchar(16);not null;default:'all'"`
	// KnowledgeBaseIDs stores the selected scope when KnowledgeBaseAccessMode is selected.
	KnowledgeBaseIDs StringArray `json:"knowledge_base_ids" gorm:"column:knowledge_base_ids;type:json"`
	// BidReview role from SSO, used by embedded-mode permission gates.
	BidReviewRole string `json:"bidreview_role,omitempty" gorm:"column:bidreview_role;type:varchar(32);default:'member'"`
	// Creation time of the user
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the user
	UpdatedAt time.Time `json:"updated_at"`
	// Deletion time of the user
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`

	// Association relationship, not stored in the database
	Tenant *Tenant `json:"tenant,omitempty" gorm:"foreignKey:TenantID"`
}

// EffectiveRole normalizes persisted and legacy role data at the database boundary.
func (u *User) EffectiveRole() UserRole {
	if u == nil {
		return UserRoleMember
	}
	if u.CanAccessAllTenants || u.BidReviewRole == string(UserRolePlatformAdmin) {
		return UserRolePlatformAdmin
	}
	if u.BidReviewRole == string(UserRoleTenantAdmin) {
		return UserRoleTenantAdmin
	}
	switch UserRole(strings.TrimSpace(string(u.Role))) {
	case UserRolePlatformAdmin:
		return UserRolePlatformAdmin
	case UserRoleTenantAdmin:
		return UserRoleTenantAdmin
	case UserRoleMember:
		return UserRoleMember
	}
	if u.Role == "" && u.BidReviewRole == "" {
		return UserRoleTenantAdmin
	}
	return UserRoleMember
}

func (u *User) IsPlatformAdmin() bool {
	return u.EffectiveRole() == UserRolePlatformAdmin
}

func (u *User) CanManageTenant() bool {
	role := u.EffectiveRole()
	return role == UserRolePlatformAdmin || role == UserRoleTenantAdmin
}

func (u *User) EffectiveKnowledgeBaseAccessMode() KnowledgeBaseAccessMode {
	if u == nil || u.CanManageTenant() {
		return KnowledgeBaseAccessAll
	}
	if u.KnowledgeBaseAccessMode == KnowledgeBaseAccessSelected {
		return KnowledgeBaseAccessSelected
	}
	return KnowledgeBaseAccessAll
}

func (u *User) CanAccessKnowledgeBase(knowledgeBaseID string) bool {
	if u == nil || strings.TrimSpace(knowledgeBaseID) == "" {
		return false
	}
	if u.EffectiveKnowledgeBaseAccessMode() == KnowledgeBaseAccessAll {
		return true
	}
	return stringArrayContains(u.KnowledgeBaseIDs, knowledgeBaseID)
}

// AuthToken represents an authentication token
type AuthToken struct {
	// Unique identifier of the token
	ID string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	// User ID that owns this token
	UserID string `json:"user_id"    gorm:"type:varchar(36);index;not null"`
	// Token value (JWT or other format)
	Token string `json:"token"      gorm:"type:text;not null"`
	// Token type (access_token, refresh_token)
	TokenType string `json:"token_type" gorm:"type:varchar(50);not null"`
	// Token expiration time
	ExpiresAt time.Time `json:"expires_at"`
	// Whether the token is revoked
	IsRevoked bool `json:"is_revoked" gorm:"default:false"`
	// Creation time of the token
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the token
	UpdatedAt time.Time `json:"updated_at"`

	// Association relationship
	User *User `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username" binding:"required,min=2,max=100"`
	Password string `json:"password" binding:"required,min=6"`
}

type OIDCAuthURLResponse struct {
	Success             bool   `json:"success"`
	ProviderDisplayName string `json:"provider_display_name,omitempty"`
	AuthorizationURL    string `json:"authorization_url,omitempty"`
	State               string `json:"state,omitempty"`
}

type OIDCConfigResponse struct {
	Success             bool   `json:"success"`
	Enabled             bool   `json:"enabled"`
	ProviderDisplayName string `json:"provider_display_name,omitempty"`
}

type OIDCCallbackResponse struct {
	Success      bool    `json:"success"`
	Message      string  `json:"message,omitempty"`
	User         *User   `json:"user,omitempty"`
	Tenant       *Tenant `json:"tenant,omitempty"`
	Token        string  `json:"token,omitempty"`
	RefreshToken string  `json:"refresh_token,omitempty"`
	IsNewUser    bool    `json:"is_new_user,omitempty"`
}

type OIDCUserInfo struct {
	Subject  string                 `json:"subject,omitempty"`
	Username string                 `json:"username,omitempty"`
	Email    string                 `json:"email,omitempty"`
	Claims   map[string]interface{} `json:"claims,omitempty"`
}

type BidReviewSSORequest struct {
	TenantExternalID string `json:"tenant_external_id" binding:"required"`
	TenantName       string `json:"tenant_name"        binding:"required"`
	UserExternalID   string `json:"user_external_id"   binding:"required"`
	Email            string `json:"email"              binding:"required,email"`
	Username         string `json:"username"           binding:"required"`
	BidReviewRole    string `json:"bidreview_role"`
}

// RegisterRequest represents a registration request
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=2,max=50"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Success       bool    `json:"success"`
	Message       string  `json:"message,omitempty"`
	User          *User   `json:"user,omitempty"`
	Tenant        *Tenant `json:"tenant,omitempty"`
	Token         string  `json:"token,omitempty"`
	RefreshToken  string  `json:"refresh_token,omitempty"`
	BidReviewRole string  `json:"bidreview_role,omitempty"`
}

// RegisterResponse represents a registration response
type RegisterResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message,omitempty"`
	User    *User   `json:"user,omitempty"`
	Tenant  *Tenant `json:"tenant,omitempty"`
}

// UserInfo represents user information for API responses
type UserInfo struct {
	ID                      string                  `json:"id"`
	Username                string                  `json:"username"`
	Nickname                string                  `json:"nickname"`
	Email                   string                  `json:"email"`
	Avatar                  string                  `json:"avatar"`
	TenantID                uint64                  `json:"tenant_id"`
	IsActive                bool                    `json:"is_active"`
	CanAccessAllTenants     bool                    `json:"can_access_all_tenants"`
	Role                    UserRole                `json:"role"`
	KnowledgeBaseAccessMode KnowledgeBaseAccessMode `json:"knowledge_base_access_mode"`
	KnowledgeBaseIDs        StringArray             `json:"knowledge_base_ids"`
	CanDelete               bool                    `json:"can_delete"`
	BidReviewRole           string                  `json:"bidreview_role,omitempty"`
	CreatedAt               time.Time               `json:"created_at"`
	UpdatedAt               time.Time               `json:"updated_at"`
}

// ToUserInfo converts User to UserInfo (without sensitive data)
func (u *User) ToUserInfo() *UserInfo {
	return &UserInfo{
		ID:                      u.ID,
		Username:                u.Username,
		Nickname:                u.Nickname,
		Email:                   u.Email,
		Avatar:                  u.Avatar,
		TenantID:                u.TenantID,
		IsActive:                u.IsActive,
		CanAccessAllTenants:     u.CanAccessAllTenants,
		Role:                    u.EffectiveRole(),
		KnowledgeBaseAccessMode: u.EffectiveKnowledgeBaseAccessMode(),
		KnowledgeBaseIDs:        u.KnowledgeBaseIDs,
		BidReviewRole:           u.BidReviewRole,
		CreatedAt:               u.CreatedAt,
		UpdatedAt:               u.UpdatedAt,
	}
}
