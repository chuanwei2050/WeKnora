package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrTokenNotFound     = errors.New("token not found")
)

// userRepository implements user repository interface
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new user repository
func NewUserRepository(db *gorm.DB) interfaces.UserRepository {
	return &userRepository{db: db}
}

// CreateUser creates a user
func (r *userRepository) CreateUser(ctx context.Context, user *types.User) error {
	if strings.TrimSpace(user.Nickname) == "" {
		user.Nickname = user.Username
	}
	if !types.IsKnowledgeBaseAccessMode(user.KnowledgeBaseAccessMode) {
		user.KnowledgeBaseAccessMode = types.KnowledgeBaseAccessAll
	}
	if user.KnowledgeBaseIDs == nil {
		user.KnowledgeBaseIDs = types.StringArray{}
	}
	// Native users intentionally keep an empty BidReviewRole. Preserve that
	// classification before create because GORM or the database may apply the
	// SSO member default to an empty value.
	isNativeUser := user.BidReviewRole == ""
	if err := r.db.WithContext(ctx).Select("*").Create(user).Error; err != nil {
		return err
	}
	if !isNativeUser {
		return nil
	}
	if err := r.db.WithContext(ctx).
		Model(&types.User{}).
		Where("id = ?", user.ID).
		UpdateColumn("bidreview_role", "").Error; err != nil {
		return err
	}
	user.BidReviewRole = ""
	return nil
}

// GetUserByID gets a user by ID
func (r *userRepository) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	var user types.User
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail gets a user by email
func (r *userRepository) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	var user types.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername gets a user by username
func (r *userRepository) GetUserByUsername(ctx context.Context, username string) (*types.User, error) {
	var user types.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByTenantID gets the first user (owner) of a tenant
func (r *userRepository) GetUserByTenantID(ctx context.Context, tenantID uint64) (*types.User, error) {
	var user types.User
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("created_at ASC").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

// UpdateUser updates a user
func (r *userRepository) UpdateUser(ctx context.Context, user *types.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

// DeleteUser deletes a user
func (r *userRepository) DeleteUser(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.User{}).Error
}

// ListUsers lists users with pagination
func (r *userRepository) ListUsers(ctx context.Context, offset, limit int) ([]*types.User, error) {
	var users []*types.User
	query := r.db.WithContext(ctx).Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// SearchUsers searches users by username or email
func (r *userRepository) SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error) {
	var users []*types.User
	searchPattern := "%" + query + "%"

	dbQuery := r.db.WithContext(ctx).
		Where("username ILIKE ? OR email ILIKE ?", searchPattern, searchPattern).
		Where("is_active = ?", true).
		Order("username ASC")

	if limit > 0 {
		dbQuery = dbQuery.Limit(limit)
	} else {
		dbQuery = dbQuery.Limit(20) // default limit
	}

	if err := dbQuery.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) ListUsersByTenant(ctx context.Context, tenantID uint64, query string, offset, limit int) ([]*types.User, int64, error) {
	var users []*types.User
	dbQuery := r.db.WithContext(ctx).Model(&types.User{}).Where("tenant_id = ?", tenantID)
	if keyword := strings.TrimSpace(query); keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		dbQuery = dbQuery.Where("LOWER(nickname) LIKE ? OR LOWER(username) LIKE ? OR LOWER(email) LIKE ?", pattern, pattern, pattern)
	}
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit > 0 {
		dbQuery = dbQuery.Limit(limit)
	}
	if offset > 0 {
		dbQuery = dbQuery.Offset(offset)
	}
	if err := dbQuery.Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

func (r *userRepository) CountActiveTenantAdmins(ctx context.Context, tenantID uint64, excludeUserID string) (int64, error) {
	query := r.db.WithContext(ctx).Model(&types.User{}).
		Where("tenant_id = ? AND role = ? AND is_active = ?", tenantID, types.UserRoleTenantAdmin, true)
	if excludeUserID != "" {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *userRepository) HasUserDocumentActivity(ctx context.Context, userID string) (bool, error) {
	checks := []struct {
		model interface{}
		field string
	}{
		{model: &types.KnowledgeBase{}, field: "created_by"},
		{model: &types.Knowledge{}, field: "created_by"},
		{model: &types.KnowledgeVersion{}, field: "created_by"},
		{model: &types.KnowledgeVersionReview{}, field: "reviewer_id"},
	}
	for _, check := range checks {
		if !r.db.Migrator().HasTable(check.model) {
			continue
		}
		var count int64
		if err := r.db.WithContext(ctx).Unscoped().Model(check.model).Where(check.field+" = ?", userID).Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

// authTokenRepository implements auth token repository interface
type authTokenRepository struct {
	db *gorm.DB
}

// NewAuthTokenRepository creates a new auth token repository
func NewAuthTokenRepository(db *gorm.DB) interfaces.AuthTokenRepository {
	return &authTokenRepository{db: db}
}

// CreateToken creates an auth token
func (r *authTokenRepository) CreateToken(ctx context.Context, token *types.AuthToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

// GetTokenByValue gets a token by its value
func (r *authTokenRepository) GetTokenByValue(ctx context.Context, tokenValue string) (*types.AuthToken, error) {
	var token types.AuthToken
	if err := r.db.WithContext(ctx).Where("token = ?", tokenValue).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return &token, nil
}

// GetTokensByUserID gets all tokens for a user
func (r *authTokenRepository) GetTokensByUserID(ctx context.Context, userID string) ([]*types.AuthToken, error) {
	var tokens []*types.AuthToken
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&tokens).Error; err != nil {
		return nil, err
	}
	return tokens, nil
}

// UpdateToken updates a token
func (r *authTokenRepository) UpdateToken(ctx context.Context, token *types.AuthToken) error {
	return r.db.WithContext(ctx).Save(token).Error
}

// DeleteToken deletes a token
func (r *authTokenRepository) DeleteToken(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&types.AuthToken{}).Error
}

// DeleteExpiredTokens deletes all expired tokens
func (r *authTokenRepository) DeleteExpiredTokens(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at < NOW()").Delete(&types.AuthToken{}).Error
}

// RevokeTokensByUserID revokes all tokens for a user
func (r *authTokenRepository) RevokeTokensByUserID(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Model(&types.AuthToken{}).Where("user_id = ?", userID).Update("is_revoked", true).Error
}
