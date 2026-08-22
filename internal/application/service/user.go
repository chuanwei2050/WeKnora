package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

var loginUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,99}$`)

type oidcAuthorizationState struct {
	Nonce       string `json:"nonce"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

var (
	jwtSecretOnce sync.Once
	jwtSecret     string
)

// getJwtSecret retrieves the JWT secret from the environment, falling back to a securely generated random secret.
func getJwtSecret() string {
	jwtSecretOnce.Do(func() {
		if envSecret := strings.TrimSpace(os.Getenv("JWT_SECRET")); envSecret != "" {
			jwtSecret = envSecret
			return
		}

		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			panic(fmt.Sprintf("failed to generate JWT secret: %v", err))
		}
		jwtSecret = base64.StdEncoding.EncodeToString(randomBytes)
	})

	return jwtSecret
}

// userService implements the UserService interface
type userService struct {
	userRepo      interfaces.UserRepository
	tokenRepo     interfaces.AuthTokenRepository
	tenantService interfaces.TenantService
	kbRepo        interfaces.KnowledgeBaseRepository
	config        *config.Config
}

// NewUserService creates a new user service instance
func NewUserService(
	configInfo *config.Config,
	userRepo interfaces.UserRepository,
	tokenRepo interfaces.AuthTokenRepository,
	tenantService interfaces.TenantService,
	kbRepo interfaces.KnowledgeBaseRepository,
) interfaces.UserService {
	return &userService{
		userRepo:      userRepo,
		tokenRepo:     tokenRepo,
		tenantService: tenantService,
		kbRepo:        kbRepo,
		config:        configInfo,
	}
}

const (
	defaultAdminUsername = "admin"
	defaultAdminEmail    = "admin@weknora.local"
	defaultAdminPassword = "Admin@123456"
)

// EnsureDefaultAdmin creates the configured bootstrap administrator only for an empty installation.
func (s *userService) EnsureDefaultAdmin(ctx context.Context) error {
	if value := strings.TrimSpace(os.Getenv("DEFAULT_ADMIN_ENABLED")); value != "" && !strings.EqualFold(value, "true") {
		return nil
	}

	users, err := s.userRepo.ListUsers(ctx, 0, 1)
	if err != nil {
		return fmt.Errorf("check whether default administrator bootstrap is needed: %w", err)
	}
	if len(users) > 0 {
		return nil
	}

	username := envOrDefault("DEFAULT_ADMIN_USERNAME", defaultAdminUsername)
	email := envOrDefault("DEFAULT_ADMIN_EMAIL", defaultAdminEmail)
	password := envOrDefault("DEFAULT_ADMIN_PASSWORD", defaultAdminPassword)

	admin, err := s.userRepo.GetUserByUsername(ctx, username)
	if err == nil && admin != nil {
		return s.ensurePlatformAdminRole(ctx, admin)
	}
	if err != nil && !isUserLookupNotFound(err) {
		return fmt.Errorf("check default administrator by username: %w", err)
	}

	admin, err = s.userRepo.GetUserByEmail(ctx, email)
	if err == nil && admin != nil {
		hashedPassword, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("reset legacy default administrator password: %w", hashErr)
		}
		admin.Username = username
		admin.PasswordHash = string(hashedPassword)
		if roleErr := s.ensurePlatformAdminRole(ctx, admin); roleErr != nil {
			return roleErr
		}
		logger.Warnf(ctx, "Legacy default administrator normalized to username %s; change the default password before production use", secutils.SanitizeForLog(username))
		return nil
	}
	if err != nil && !isUserLookupNotFound(err) {
		return fmt.Errorf("check default administrator by email: %w", err)
	}

	admin, err = s.Register(ctx, &types.RegisterRequest{
		Username: username,
		Email:    email,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("create default administrator: %w", err)
	}
	if err := s.ensurePlatformAdminRole(ctx, admin); err != nil {
		return err
	}

	logger.Warnf(ctx, "Default administrator created for %s; change the default password before production use", secutils.SanitizeForLog(email))
	return nil
}

func (s *userService) ensurePlatformAdminRole(ctx context.Context, admin *types.User) error {
	if admin.Role == types.UserRolePlatformAdmin && admin.CanAccessAllTenants {
		return nil
	}
	admin.Role = types.UserRolePlatformAdmin
	admin.CanAccessAllTenants = true
	admin.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, admin); err != nil {
		return fmt.Errorf("grant platform administrator role: %w", err)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// Register creates a new user account
func (s *userService) Register(ctx context.Context, req *types.RegisterRequest) (*types.User, error) {
	logger.Info(ctx, "Start user registration")

	// Validate input
	if req.Username == "" || req.Email == "" || req.Password == "" {
		return nil, errors.New("username, email and password are required")
	}

	// Check if user already exists
	existingUser, _ := s.userRepo.GetUserByEmail(ctx, req.Email)
	if existingUser != nil {
		return nil, errors.New("user with this email already exists")
	}

	existingUser, _ = s.userRepo.GetUserByUsername(ctx, req.Username)
	if existingUser != nil {
		return nil, errors.New("user with this username already exists")
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Errorf(ctx, "Failed to hash password: %v", err)
		return nil, errors.New("failed to process password")
	}

	// Create default tenant for the user
	// Note: RetrieverEngines is left empty - system will use defaults from RETRIEVE_DRIVER env
	tenant := &types.Tenant{
		Name:        fmt.Sprintf("%s's Workspace", secutils.SanitizeForLog(req.Username)),
		Description: "Default workspace",
		Status:      "active",
	}

	createdTenant, err := s.tenantService.CreateTenant(ctx, tenant)
	if err != nil {
		logger.Errorf(ctx, "Failed to create tenant")
		return nil, errors.New("failed to create workspace")
	}

	// Create user
	user := &types.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		TenantID:     createdTenant.ID,
		IsActive:     true,
		Role:         types.UserRoleTenantAdmin,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	err = s.userRepo.CreateUser(ctx, user)
	if err != nil {
		logger.Errorf(ctx, "Failed to create user: %v", err)
		return nil, errors.New("failed to create user")
	}

	logger.Info(ctx, "User registered successfully")
	return user, nil
}

// Login authenticates a user and returns tokens
func (s *userService) Login(ctx context.Context, req *types.LoginRequest) (*types.LoginResponse, error) {
	logger.Info(ctx, "Start user login")
	// Get user by username
	user, err := s.userRepo.GetUserByUsername(ctx, req.Username)
	if err != nil {
		logger.Errorf(ctx, "Failed to get user by username: %v", err)
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		}, nil
	}
	if user == nil {
		logger.Warn(ctx, "User not found for username")
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		}, nil
	}

	// Check if user is active
	if !user.IsActive {
		logger.Warn(ctx, "User account is disabled")
		return &types.LoginResponse{
			Success: false,
			Message: "Account is disabled",
		}, nil
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		logger.Warn(ctx, "Password verification failed")
		return &types.LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		}, nil
	}
	logger.Info(ctx, "Password verification successful")

	// Verify the tenant before issuing any token.
	tenant, err := s.tenantService.GetTenantByID(ctx, user.TenantID)
	if err != nil || tenant == nil || tenant.Status != string(types.TenantStatusActive) {
		logger.Warn(ctx, "User tenant is unavailable or suspended")
		return &types.LoginResponse{
			Success: false,
			Message: "Tenant is suspended",
		}, nil
	}

	// Generate tokens
	logger.Info(ctx, "Generating tokens")
	accessToken, refreshToken, err := s.GenerateTokens(ctx, user)
	if err != nil {
		logger.Errorf(ctx, "Failed to generate tokens: %v", err)
		return &types.LoginResponse{Success: false, Message: "Login failed"}, nil
	}
	logger.Info(ctx, "Tokens generated successfully")
	logger.Info(ctx, "Tenant information retrieved successfully")

	logger.Info(ctx, "User logged in successfully")
	return &types.LoginResponse{
		Success:      true,
		Message:      "Login successful",
		User:         user,
		Tenant:       tenant,
		Token:        accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// GetOIDCAuthorizationURL builds the OIDC authorization URL.
func (s *userService) GetOIDCAuthorizationURL(ctx context.Context, redirectURI string) (*types.OIDCAuthURLResponse, error) {
	cfg, err := s.getOIDCConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("redirect_uri is required")
	}

	nonce, err := generateRandomString(24)
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	state, err := encodeOIDCAuthorizationState(&oidcAuthorizationState{
		Nonce:       nonce,
		RedirectURI: strings.TrimSpace(redirectURI),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode OIDC state: %w", err)
	}

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(cfg.Scopes, " "))
	query.Set("state", state)

	authURL := cfg.AuthorizationEndpoint
	if strings.Contains(authURL, "?") {
		authURL += "&" + query.Encode()
	} else {
		authURL += "?" + query.Encode()
	}

	return &types.OIDCAuthURLResponse{
		Success:             true,
		ProviderDisplayName: cfg.ProviderDisplayName,
		AuthorizationURL:    authURL,
		State:               state,
	}, nil
}

// LoginWithOIDC exchanges code for tokens, loads user info, provisions user if needed, and returns local login tokens.
func (s *userService) LoginWithOIDC(ctx context.Context, code, redirectURI string) (*types.OIDCCallbackResponse, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("code is required")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("redirect_uri is required")
	}

	cfg, err := s.getOIDCConfig(ctx)
	if err != nil {
		return nil, err
	}

	tokenResp, err := s.exchangeOIDCCode(ctx, cfg, code, redirectURI)
	if err != nil {
		return nil, err
	}

	userInfo, err := s.resolveOIDCUserInfo(ctx, cfg, tokenResp)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(userInfo.Email) == "" {
		return nil, errors.New("OIDC provider did not return email")
	}

	user, err := s.userRepo.GetUserByEmail(ctx, userInfo.Email)
	if err != nil && !isUserLookupNotFound(err) {
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}
	isNewUser := false
	if isUserLookupNotFound(err) || user == nil {
		user, err = s.provisionOIDCUser(ctx, userInfo)
		if err != nil {
			return nil, err
		}
		isNewUser = true
	}

	if !user.IsActive {
		return &types.OIDCCallbackResponse{Success: false, Message: "Account is disabled"}, nil
	}

	accessToken, refreshToken, err := s.GenerateTokens(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate local tokens: %w", err)
	}

	return &types.OIDCCallbackResponse{
		Success:      true,
		Message:      "登录成功",
		Token:        accessToken,
		RefreshToken: refreshToken,
		IsNewUser:    isNewUser,
	}, nil
}

func (s *userService) LoginWithBidReviewSSO(
	ctx context.Context,
	req *types.BidReviewSSORequest,
) (*types.LoginResponse, error) {
	if req == nil {
		return nil, errors.New("request is required")
	}
	tenantExternalID := strings.TrimSpace(req.TenantExternalID)
	userExternalID := strings.TrimSpace(req.UserExternalID)
	sourceEmail := strings.TrimSpace(strings.ToLower(req.Email))
	sourceUsername := strings.TrimSpace(req.Username)
	bidReviewRole := normalizeBidReviewRole(req.BidReviewRole)
	if tenantExternalID == "" || userExternalID == "" || sourceEmail == "" || sourceUsername == "" {
		return nil, errors.New("tenant_external_id, user_external_id, email and username are required")
	}

	businessKey := "bidreview:" + tenantExternalID
	tenant, err := s.findTenantByBusiness(ctx, businessKey)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		tenantName := strings.TrimSpace(req.TenantName)
		if tenantName == "" {
			tenantName = "BidReview Workspace"
		}
		tenant, err = s.tenantService.CreateTenant(ctx, &types.Tenant{
			Name:        tenantName,
			Description: "Provisioned from BidReview",
			Business:    businessKey,
			Status:      "active",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create BidReview tenant: %w", err)
		}
	}

	email := bidReviewSyntheticEmail(tenantExternalID, userExternalID)
	user, err := s.userRepo.GetUserByEmail(ctx, email)
	if err != nil && !isUserLookupNotFound(err) {
		return nil, fmt.Errorf("failed to query BidReview user: %w", err)
	}
	if isUserLookupNotFound(err) || user == nil {
		username := s.generateBidReviewUsername(ctx, sourceUsername, tenantExternalID, userExternalID)
		randomPassword, err := generateRandomString(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate password: %w", err)
		}
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		user = &types.User{
			ID:            uuid.New().String(),
			Username:      username,
			Email:         email,
			PasswordHash:  string(hashedPassword),
			TenantID:      tenant.ID,
			IsActive:      true,
			Role:          types.UserRole(bidReviewRole),
			BidReviewRole: bidReviewRole,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := s.userRepo.CreateUser(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to create BidReview user: %w", err)
		}
	} else {
		changed := false
		if user.TenantID != tenant.ID {
			user.TenantID = tenant.ID
			changed = true
		}
		if !user.IsActive {
			user.IsActive = true
			changed = true
		}
		if user.BidReviewRole != bidReviewRole {
			user.BidReviewRole = bidReviewRole
			changed = true
		}
		if user.Role != types.UserRole(bidReviewRole) {
			user.Role = types.UserRole(bidReviewRole)
			changed = true
		}
		if changed {
			user.UpdatedAt = time.Now()
			if err := s.userRepo.UpdateUser(ctx, user); err != nil {
				return nil, fmt.Errorf("failed to update BidReview user: %w", err)
			}
		}
	}

	user.BidReviewRole = bidReviewRole
	accessToken, refreshToken, err := s.GenerateTokens(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}
	return &types.LoginResponse{
		Success:       true,
		Message:       "Login successful",
		User:          user,
		Tenant:        tenant,
		Token:         accessToken,
		RefreshToken:  refreshToken,
		BidReviewRole: bidReviewRole,
	}, nil
}

func normalizeBidReviewRole(role string) string {
	switch strings.TrimSpace(role) {
	case "platform_admin":
		return "platform_admin"
	case "tenant_admin":
		return "tenant_admin"
	default:
		return "member"
	}
}

// GetUserByID gets a user by ID
func (s *userService) GetUserByID(ctx context.Context, id string) (*types.User, error) {
	return s.userRepo.GetUserByID(ctx, id)
}

// GetUserByEmail gets a user by email
func (s *userService) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	return s.userRepo.GetUserByEmail(ctx, email)
}

// GetUserByUsername gets a user by username
func (s *userService) GetUserByUsername(ctx context.Context, username string) (*types.User, error) {
	return s.userRepo.GetUserByUsername(ctx, username)
}

// GetUserByTenantID gets the first user (owner) of a tenant
func (s *userService) GetUserByTenantID(ctx context.Context, tenantID uint64) (*types.User, error) {
	return s.userRepo.GetUserByTenantID(ctx, tenantID)
}

// UpdateUser updates user information
func (s *userService) UpdateUser(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}
	return s.tokenRepo.RevokeTokensByUserID(ctx, user.ID)
}

// DeleteUser deletes a user
func (s *userService) DeleteUser(ctx context.Context, id string) error {
	return s.userRepo.DeleteUser(ctx, id)
}

func authorizeTenantUserManagement(actor *types.User, tenantID uint64) error {
	if actor == nil {
		return werrors.NewUnauthorizedError("Authentication required")
	}
	if !actor.CanManageTenant() {
		return werrors.NewForbiddenError("Tenant administrator permission required")
	}
	if !actor.IsPlatformAdmin() && actor.TenantID != tenantID {
		return werrors.NewForbiddenError("Cross-tenant user management is forbidden")
	}
	return nil
}

func (s *userService) tenantUser(ctx context.Context, tenantID uint64, userID string) (*types.User, error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, werrors.NewNotFoundError("User not found")
	}
	if user.TenantID != tenantID {
		return nil, werrors.NewForbiddenError("Cross-tenant user management is forbidden")
	}
	if user.IsPlatformAdmin() {
		return nil, werrors.NewForbiddenError("Platform administrators cannot be changed through tenant user management")
	}
	return user, nil
}

func (s *userService) ListTenantUsers(ctx context.Context, actor *types.User, tenantID uint64, query string, offset, limit int) ([]*types.User, int64, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.userRepo.ListUsersByTenant(ctx, tenantID, query, offset, limit)
}

func (s *userService) CreateTenantUser(ctx context.Context, actor *types.User, tenantID uint64, req *types.CreateTenantUserRequest) (*types.User, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return nil, err
	}
	if req == nil || !types.IsTenantUserRole(req.Role) {
		return nil, werrors.NewValidationError("Role must be tenant_admin or member")
	}
	if !actor.IsPlatformAdmin() && req.Role != types.UserRoleMember {
		return nil, werrors.NewForbiddenError("Tenant administrators can only create member accounts")
	}
	if err := validateManagedPassword(req.Password); err != nil {
		return nil, err
	}
	username := strings.TrimSpace(req.Username)
	if err := validateManagedUsername(username); err != nil {
		return nil, err
	}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" {
		nickname = username
	}
	if err := validateManagedNickname(nickname); err != nil {
		return nil, err
	}
	if existing, err := s.userRepo.GetUserByUsername(ctx, username); err == nil && existing != nil {
		return nil, werrors.NewConflictError("Username already exists")
	} else if err != nil && !isUserLookupNotFound(err) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	mode := req.KnowledgeBaseAccessMode
	ids := types.StringArray{}
	if req.Role == types.UserRoleMember {
		if mode == "" {
			mode = types.KnowledgeBaseAccessAll
		}
		ids, err = s.validateKnowledgeBaseScope(ctx, tenantID, mode, req.KnowledgeBaseIDs)
		if err != nil {
			return nil, err
		}
	} else {
		mode = types.KnowledgeBaseAccessAll
	}
	now := time.Now()
	userID := uuid.NewString()
	user := &types.User{
		ID: userID, Username: username, Nickname: nickname, Email: userID + "@users.weknora.invalid", PasswordHash: string(hash),
		TenantID: tenantID, IsActive: true, Role: req.Role, KnowledgeBaseAccessMode: mode,
		KnowledgeBaseIDs: ids, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func validateManagedPassword(password string) error {
	if password == "" {
		return nil
	}
	if len(password) < 8 || len(password) > 72 {
		return werrors.NewValidationError("Password must be 8 to 72 characters")
	}
	hasLetter, hasDigit := false, false
	for _, r := range password {
		if unicode.IsSpace(r) {
			return werrors.NewValidationError("Password cannot contain whitespace")
		}
		if r < '!' || r > '~' {
			return werrors.NewValidationError("Password may only contain ASCII letters, numbers and special characters")
		}
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return werrors.NewValidationError("Password must contain both letters and numbers")
	}
	return nil
}

func validateManagedUsername(username string) error {
	if !loginUsernamePattern.MatchString(strings.TrimSpace(username)) {
		return werrors.NewValidationError("Username must be 2-100 characters and contain only letters, numbers, dots, underscores or hyphens")
	}
	return nil
}

func validateManagedNickname(nickname string) error {
	length := utf8.RuneCountInString(nickname)
	if length < 1 || length > 100 {
		return werrors.NewValidationError("Nickname must be 1 to 100 characters")
	}
	return nil
}

func normalizeKnowledgeBaseIDs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, werrors.NewValidationError("Knowledge base IDs cannot be empty")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func validateManagedRoleChange(currentRole, nextRole types.UserRole) error {
	if !types.IsTenantUserRole(nextRole) {
		return werrors.NewValidationError("Role must be tenant_admin or member")
	}
	// Platform admins stay immutable; tenant_admin ↔ member is allowed
	// (last active tenant admin is guarded separately).
	if currentRole == types.UserRolePlatformAdmin || !types.IsTenantUserRole(currentRole) {
		return werrors.NewForbiddenError("Administrator roles cannot be changed")
	}
	return nil
}

func (s *userService) validateKnowledgeBaseScope(ctx context.Context, tenantID uint64, mode types.KnowledgeBaseAccessMode, values []string) (types.StringArray, error) {
	if !types.IsKnowledgeBaseAccessMode(mode) {
		return nil, werrors.NewValidationError("Knowledge base access mode must be all or selected")
	}
	if mode == types.KnowledgeBaseAccessAll {
		return types.StringArray{}, nil
	}
	ids, err := normalizeKnowledgeBaseIDs(values)
	if err != nil || len(ids) == 0 {
		return types.StringArray(ids), err
	}
	kbs, err := s.kbRepo.GetKnowledgeBaseByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(kbs) != len(ids) {
		return nil, werrors.NewValidationError("One or more knowledge bases do not exist")
	}
	for _, kb := range kbs {
		if kb == nil || kb.TenantID != tenantID {
			return nil, werrors.NewForbiddenError("Knowledge base belongs to another tenant")
		}
	}
	return types.StringArray(ids), nil
}

func (s *userService) UpdateTenantUser(ctx context.Context, actor *types.User, tenantID uint64, userID string, req *types.UpdateTenantUserRequest) (*types.User, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return nil, err
	}
	if req == nil || !types.IsTenantUserRole(req.Role) {
		return nil, werrors.NewValidationError("Role must be tenant_admin or member")
	}
	username := strings.TrimSpace(req.Username)
	if err := validateManagedUsername(username); err != nil {
		return nil, err
	}
	nickname := strings.TrimSpace(req.Nickname)
	if nickname == "" {
		nickname = username
	}
	if err := validateManagedNickname(nickname); err != nil {
		return nil, err
	}
	if err := validateManagedPassword(req.Password); err != nil {
		return nil, err
	}
	user, err := s.tenantUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	currentRole := user.EffectiveRole()
	if err := validateManagedRoleChange(currentRole, req.Role); err != nil {
		return nil, err
	}
	if currentRole == types.UserRoleTenantAdmin && req.Role != types.UserRoleTenantAdmin {
		if err := s.ensureNotLastTenantAdmin(ctx, user); err != nil {
			return nil, err
		}
	}
	if existing, lookupErr := s.userRepo.GetUserByUsername(ctx, username); lookupErr == nil && existing != nil && existing.ID != user.ID {
		return nil, werrors.NewConflictError("Username already exists")
	} else if lookupErr != nil && !isUserLookupNotFound(lookupErr) {
		return nil, lookupErr
	}

	mode := req.KnowledgeBaseAccessMode
	ids := types.StringArray{}
	if req.Role == types.UserRoleMember {
		ids, err = s.validateKnowledgeBaseScope(ctx, tenantID, mode, req.KnowledgeBaseIDs)
		if err != nil {
			return nil, err
		}
	} else {
		mode = types.KnowledgeBaseAccessAll
	}

	user.Username = username
	user.Nickname = nickname
	applyManagedTenantRole(user, req.Role)
	user.KnowledgeBaseAccessMode = mode
	user.KnowledgeBaseIDs = ids
	if req.Password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return nil, hashErr
		}
		user.PasswordHash = string(hash)
	}
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	if req.Password != "" || req.Role != currentRole {
		if err := s.tokenRepo.RevokeTokensByUserID(ctx, user.ID); err != nil {
			return nil, err
		}
	}
	return user, nil
}

func (s *userService) CanDeleteTenantUser(ctx context.Context, actor *types.User, tenantID uint64, userID string) (bool, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return false, err
	}
	user, err := s.tenantUser(ctx, tenantID, userID)
	if err != nil {
		return false, err
	}
	if user.ID == actor.ID || user.EffectiveRole() != types.UserRoleMember {
		return false, nil
	}
	hasActivity, err := s.userRepo.HasUserDocumentActivity(ctx, user.ID)
	if err != nil {
		return false, err
	}
	return !hasActivity, nil
}

func (s *userService) DeleteTenantUser(ctx context.Context, actor *types.User, tenantID uint64, userID string) error {
	canDelete, err := s.CanDeleteTenantUser(ctx, actor, tenantID, userID)
	if err != nil {
		return err
	}
	if !canDelete {
		return werrors.NewConflictError("User has document activity or cannot be deleted")
	}
	if err := s.tokenRepo.RevokeTokensByUserID(ctx, userID); err != nil {
		return err
	}
	return s.userRepo.DeleteUser(ctx, userID)
}

func (s *userService) SetTenantAdminCredentials(ctx context.Context, actor *types.User, tenantID uint64, username, password string) (*types.User, error) {
	if actor == nil || !actor.IsPlatformAdmin() {
		return nil, werrors.NewForbiddenError("Platform administrator permission required")
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = "tenant_admin_" + strconv.FormatUint(tenantID, 10)
	}
	if err := validateManagedUsername(username); err != nil {
		return nil, err
	}
	users, _, err := s.userRepo.ListUsersByTenant(ctx, tenantID, "", 0, 100)
	if err != nil {
		return nil, err
	}
	var admin *types.User
	for _, user := range users {
		if user.Role == types.UserRoleTenantAdmin {
			admin = user
			break
		}
	}
	if existing, lookupErr := s.userRepo.GetUserByUsername(ctx, username); lookupErr == nil && existing != nil && (admin == nil || existing.ID != admin.ID) {
		return nil, werrors.NewConflictError("Username already exists")
	} else if lookupErr != nil && !isUserLookupNotFound(lookupErr) {
		return nil, lookupErr
	}
	if admin == nil {
		if password == "" {
			password = defaultAdminPassword
		}
		return s.CreateTenantUser(ctx, actor, tenantID, &types.CreateTenantUserRequest{
			Username: username, Password: password, Role: types.UserRoleTenantAdmin,
		})
	}
	admin.Username = username
	admin.UpdatedAt = time.Now()
	if password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return nil, hashErr
		}
		admin.PasswordHash = string(hash)
	}
	if err := s.userRepo.UpdateUser(ctx, admin); err != nil {
		return nil, err
	}
	if password != "" {
		if err := s.tokenRepo.RevokeTokensByUserID(ctx, admin.ID); err != nil {
			return nil, err
		}
	}
	return admin, nil
}

func (s *userService) ResetTenantUserPassword(ctx context.Context, actor *types.User, tenantID uint64, userID, password string) error {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return err
	}
	if err := validateManagedPassword(password); err != nil {
		return err
	}
	user, err := s.tenantUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}
	return s.tokenRepo.RevokeTokensByUserID(ctx, user.ID)
}

func applyManagedTenantRole(user *types.User, role types.UserRole) {
	user.Role = role
	user.CanAccessAllTenants = false
	// Keep EffectiveRole() aligned after demotion when legacy BidReviewRole
	// still elevates the account.
	if role == types.UserRoleMember {
		switch user.BidReviewRole {
		case string(types.UserRoleTenantAdmin), string(types.UserRolePlatformAdmin):
			user.BidReviewRole = string(types.UserRoleMember)
		}
	}
}

func (s *userService) ensureNotLastTenantAdmin(ctx context.Context, user *types.User) error {
	if user.EffectiveRole() != types.UserRoleTenantAdmin || !user.IsActive {
		return nil
	}
	count, err := s.userRepo.CountActiveTenantAdmins(ctx, user.TenantID, user.ID)
	if err != nil {
		return err
	}
	if count == 0 {
		return werrors.NewConflictError("At least one active tenant administrator is required")
	}
	return nil
}

func (s *userService) UpdateTenantUserRole(ctx context.Context, actor *types.User, tenantID uint64, userID string, role types.UserRole) (*types.User, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return nil, err
	}
	if !types.IsTenantUserRole(role) {
		return nil, werrors.NewValidationError("Role must be tenant_admin or member")
	}
	user, err := s.tenantUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	currentRole := user.EffectiveRole()
	if err := validateManagedRoleChange(currentRole, role); err != nil {
		return nil, err
	}
	if currentRole == types.UserRoleTenantAdmin && role != types.UserRoleTenantAdmin {
		if err := s.ensureNotLastTenantAdmin(ctx, user); err != nil {
			return nil, err
		}
	}
	applyManagedTenantRole(user, role)
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.tokenRepo.RevokeTokensByUserID(ctx, user.ID); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *userService) UpdateTenantUserStatus(ctx context.Context, actor *types.User, tenantID uint64, userID string, active bool) (*types.User, error) {
	if err := authorizeTenantUserManagement(actor, tenantID); err != nil {
		return nil, err
	}
	user, err := s.tenantUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if !active {
		if err := s.ensureNotLastTenantAdmin(ctx, user); err != nil {
			return nil, err
		}
	}
	user.IsActive = active
	user.UpdatedAt = time.Now()
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.tokenRepo.RevokeTokensByUserID(ctx, user.ID); err != nil {
		return nil, err
	}
	return user, nil
}

// ChangePassword changes user password
func (s *userService) ChangePassword(ctx context.Context, userID string, oldPassword, newPassword string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify old password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return errors.New("invalid old password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hashedPassword)
	user.UpdatedAt = time.Now()

	return s.userRepo.UpdateUser(ctx, user)
}

// ValidatePassword validates user password
func (s *userService) ValidatePassword(ctx context.Context, userID string, password string) error {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}

	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
}

// GenerateTokens generates access and refresh tokens for user
func (s *userService) GenerateTokens(
	ctx context.Context,
	user *types.User,
) (accessToken, refreshToken string, err error) {
	// Generate access token (expires in 24 hours)
	accessClaims := jwt.MapClaims{
		"user_id":   user.ID,
		"email":     user.Email,
		"tenant_id": user.TenantID,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"type":      "access",
	}

	accessTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err = accessTokenObj.SignedString([]byte(getJwtSecret()))
	if err != nil {
		return "", "", err
	}

	// Generate refresh token (expires in 7 days)
	refreshClaims := jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
		"type":    "refresh",
	}

	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err = refreshTokenObj.SignedString([]byte(getJwtSecret()))
	if err != nil {
		return "", "", err
	}

	// Store tokens in database
	accessTokenRecord := &types.AuthToken{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     accessToken,
		TokenType: "access_token",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	refreshTokenRecord := &types.AuthToken{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		Token:     refreshToken,
		TokenType: "refresh_token",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_ = s.tokenRepo.CreateToken(ctx, accessTokenRecord)
	_ = s.tokenRepo.CreateToken(ctx, refreshTokenRecord)

	return accessToken, refreshToken, nil
}

// ValidateToken validates an access token
func (s *userService) ValidateToken(ctx context.Context, tokenString string) (*types.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(getJwtSecret()), nil
	})

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid user ID in token")
	}

	// Check if token is revoked
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, tokenString)
	if err != nil || tokenRecord == nil || tokenRecord.IsRevoked {
		return nil, errors.New("token is revoked")
	}

	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, errors.New("user account is disabled")
	}
	return user, nil
}

// RefreshToken refreshes access token using refresh token
func (s *userService) RefreshToken(
	ctx context.Context,
	refreshTokenString string,
) (accessToken, newRefreshToken string, err error) {
	token, err := jwt.Parse(refreshTokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(getJwtSecret()), nil
	})

	if err != nil || !token.Valid {
		return "", "", errors.New("invalid refresh token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid token claims")
	}

	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != "refresh" {
		return "", "", errors.New("not a refresh token")
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		return "", "", errors.New("invalid user ID in token")
	}

	// Check if token is revoked
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, refreshTokenString)
	if err != nil || tokenRecord == nil || tokenRecord.IsRevoked {
		return "", "", errors.New("refresh token is revoked")
	}

	// Get user
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if !user.IsActive {
		return "", "", errors.New("user account is disabled")
	}
	tenant, err := s.tenantService.GetTenantByID(ctx, user.TenantID)
	if err != nil || tenant == nil || tenant.Status != string(types.TenantStatusActive) {
		return "", "", errors.New("tenant is suspended")
	}

	// Revoke old refresh token
	tokenRecord.IsRevoked = true
	_ = s.tokenRepo.UpdateToken(ctx, tokenRecord)

	// Generate new tokens
	return s.GenerateTokens(ctx, user)
}

// RevokeToken revokes a token
func (s *userService) RevokeToken(ctx context.Context, tokenString string) error {
	tokenRecord, err := s.tokenRepo.GetTokenByValue(ctx, tokenString)
	if err != nil {
		return err
	}

	tokenRecord.IsRevoked = true
	tokenRecord.UpdatedAt = time.Now()

	return s.tokenRepo.UpdateToken(ctx, tokenRecord)
}

// GetCurrentUser gets current user from context
func (s *userService) GetCurrentUser(ctx context.Context) (*types.User, error) {
	user, ok := ctx.Value(types.UserContextKey).(*types.User)
	if !ok {
		return nil, errors.New("user not found in context")
	}

	return user, nil
}

// SearchUsers searches users by username or email
func (s *userService) SearchUsers(ctx context.Context, query string, limit int) ([]*types.User, error) {
	if query == "" {
		return []*types.User{}, nil
	}
	return s.userRepo.SearchUsers(ctx, query, limit)
}

type oidcDiscoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}

type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

func (s *userService) getOIDCConfig(ctx context.Context) (*config.OIDCAuthConfig, error) {
	if s.config == nil || s.config.OIDCAuth == nil || !s.config.OIDCAuth.Enable {
		return nil, errors.New("OIDC login is disabled")
	}
	cfg := *s.config.OIDCAuth
	if cfg.UserInfoMapping == nil {
		cfg.UserInfoMapping = &config.OIDCUserInfoMapping{Username: "name", Email: "email"}
	}
	if err := s.populateOIDCEndpoints(ctx, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *userService) populateOIDCEndpoints(ctx context.Context, cfg *config.OIDCAuthConfig) error {
	if strings.TrimSpace(cfg.AuthorizationEndpoint) != "" && strings.TrimSpace(cfg.TokenEndpoint) != "" {
		return nil
	}
	if strings.TrimSpace(cfg.DiscoveryURL) == "" {
		return errors.New("OIDC discovery_url or explicit endpoints are required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.DiscoveryURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create OIDC discovery request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to load OIDC discovery document: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("OIDC discovery request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc oidcDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("failed to decode OIDC discovery document: %w", err)
	}
	if cfg.AuthorizationEndpoint == "" {
		cfg.AuthorizationEndpoint = doc.AuthorizationEndpoint
	}
	if cfg.TokenEndpoint == "" {
		cfg.TokenEndpoint = doc.TokenEndpoint
	}
	if cfg.UserInfoEndpoint == "" {
		cfg.UserInfoEndpoint = doc.UserInfoEndpoint
	}
	if cfg.AuthorizationEndpoint == "" || cfg.TokenEndpoint == "" {
		return errors.New("OIDC discovery document missing required endpoints")
	}
	return nil
}

func (s *userService) exchangeOIDCCode(ctx context.Context, cfg *config.OIDCAuthConfig, code, redirectURI string) (*oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange OIDC code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("OIDC token exchange failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp oidcTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC token response: %w", err)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" && strings.TrimSpace(tokenResp.IDToken) == "" {
		return nil, errors.New("OIDC token response missing access_token and id_token")
	}
	return &tokenResp, nil
}

func (s *userService) resolveOIDCUserInfo(ctx context.Context, cfg *config.OIDCAuthConfig, tokenResp *oidcTokenResponse) (*types.OIDCUserInfo, error) {
	claims := map[string]interface{}{}

	if strings.TrimSpace(tokenResp.IDToken) != "" {
		idTokenClaims, err := decodeJWTClaims(tokenResp.IDToken)
		if err != nil {
			logger.Warnf(ctx, "Failed to decode OIDC id_token claims: %v", err)
		} else {
			for k, v := range idTokenClaims {
				claims[k] = v
			}
		}
	}

	if strings.TrimSpace(cfg.UserInfoEndpoint) != "" && strings.TrimSpace(tokenResp.AccessToken) != "" {
		userInfoClaims, err := s.fetchOIDCUserInfo(ctx, cfg.UserInfoEndpoint, tokenResp.AccessToken)
		if err != nil {
			logger.Warnf(ctx, "Failed to fetch OIDC userinfo, fallback to id_token claims: %v", err)
		} else {
			for k, v := range userInfoClaims {
				claims[k] = v
			}
		}
	}

	info := &types.OIDCUserInfo{Claims: claims}
	if sub, _ := claims["sub"].(string); sub != "" {
		info.Subject = sub
	}
	info.Username = extractClaimAsString(claims, cfg.UserInfoMapping.Username)
	info.Email = extractClaimAsString(claims, cfg.UserInfoMapping.Email)
	if info.Username == "" {
		info.Username = extractClaimAsString(claims, "preferred_username")
	}
	if info.Username == "" {
		info.Username = extractClaimAsString(claims, "name")
	}
	if info.Username == "" && info.Email != "" {
		info.Username = strings.Split(info.Email, "@")[0]
	}
	return info, nil
}

func (s *userService) fetchOIDCUserInfo(ctx context.Context, endpoint, accessToken string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("userinfo request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var claims map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func (s *userService) provisionOIDCUser(ctx context.Context, info *types.OIDCUserInfo) (*types.User, error) {
	username := s.generateOIDCUsername(ctx, info)
	randomPassword, err := generateRandomString(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate password for OIDC user: %w", err)
	}

	user, err := s.Register(ctx, &types.RegisterRequest{
		Username: username,
		Email:    info.Email,
		Password: randomPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to auto-provision OIDC user: %w", err)
	}
	return user, nil
}

func (s *userService) generateOIDCUsername(ctx context.Context, info *types.OIDCUserInfo) string {
	base := sanitizeUsernameCandidate(info.Username)
	if base == "" {
		base = sanitizeUsernameCandidate(strings.Split(info.Email, "@")[0])
	}
	if base == "" {
		base = "oidc-user"
	}

	candidate := base
	for i := 0; i < 20; i++ {
		existing, err := s.userRepo.GetUserByUsername(ctx, candidate)
		if isUserLookupNotFound(err) || (err == nil && existing == nil) {
			return candidate
		}
		if err != nil && !isUserLookupNotFound(err) {
			logger.Warnf(ctx, "Failed to check existing OIDC username %q: %v", candidate, err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i+1)
	}
	return fmt.Sprintf("%s-%d", base, time.Now().Unix())
}

func (s *userService) findTenantByBusiness(ctx context.Context, business string) (*types.Tenant, error) {
	tenants, err := s.tenantService.ListTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	for _, tenant := range tenants {
		if tenant != nil && tenant.Business == business {
			return tenant, nil
		}
	}
	return nil, nil
}

func (s *userService) generateBidReviewUsername(ctx context.Context, name, tenantExternalID, userExternalID string) string {
	base := sanitizeUsernameCandidate(name)
	if base == "" {
		base = "bidreview-user"
	}
	suffix := shortStableID(tenantExternalID) + "-" + shortStableID(userExternalID)
	candidate := fmt.Sprintf("%s-%s", base, suffix)
	if len(candidate) > 100 {
		candidate = candidate[:100]
	}
	existing, err := s.userRepo.GetUserByUsername(ctx, candidate)
	if isUserLookupNotFound(err) || (err == nil && existing == nil) {
		return candidate
	}
	return fmt.Sprintf("bidreview-%s-%s", shortStableID(tenantExternalID), shortStableID(userExternalID))
}

func bidReviewSyntheticEmail(tenantExternalID, userExternalID string) string {
	return fmt.Sprintf("br-%s-%s@bidreview.local", shortStableID(tenantExternalID), shortStableID(userExternalID))
}

func shortStableID(value string) string {
	cleaned := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(value)))
	if len(cleaned) >= 12 {
		return cleaned[:12]
	}
	if cleaned != "" {
		return cleaned
	}
	return "unknown"
}

func generateRandomString(length int) (string, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func encodeOIDCAuthorizationState(state *oidcAuthorizationState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func extractClaimAsString(claims map[string]interface{}, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	value, ok := claims[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func sanitizeUsernameCandidate(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(b.String(), "-._")
	if len(result) > 50 {
		result = strings.Trim(result[:50], "-._")
	}
	return result
}

func isUserLookupNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, apprepo.ErrUserNotFound) || strings.Contains(strings.ToLower(err.Error()), "user not found")
}
