package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	tenantService interfaces.TenantService
	userService   interfaces.UserService
	kbService     interfaces.KnowledgeBaseService
}

func NewAdminHandler(tenantService interfaces.TenantService, userService interfaces.UserService, kbService interfaces.KnowledgeBaseService) *AdminHandler {
	return &AdminHandler{tenantService: tenantService, userService: userService, kbService: kbService}
}

type adminTenantRequest struct {
	Name          string `json:"name" binding:"required,max=255"`
	Description   string `json:"description" binding:"max=1000"`
	Business      string `json:"business" binding:"max=255"`
	StorageQuota  int64  `json:"storage_quota" binding:"omitempty,min=0"`
	AdminUsername string `json:"admin_username" binding:"omitempty,min=2,max=100"`
	AdminPassword string `json:"admin_password" binding:"omitempty,min=8,max=72"`
}

type adminTenantStatusRequest struct {
	Status types.TenantStatus `json:"status" binding:"required"`
}

type adminTenantView struct {
	ID            uint64    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Business      string    `json:"business"`
	StorageQuota  int64     `json:"storage_quota"`
	StorageUsed   int64     `json:"storage_used"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CanDelete     bool      `json:"can_delete"`
	AdminUsername string    `json:"admin_username,omitempty"`
}

const defaultTenantAdminPassword = "Admin@123456"

func validateAdminTenantPassword(password string) error {
	if password == "" {
		return nil
	}
	if len(password) < 8 || len(password) > 72 {
		return werrors.NewValidationError("Administrator password must be 8 to 72 characters")
	}
	hasLetter, hasDigit := false, false
	for _, r := range password {
		if unicode.Is(unicode.Han, r) {
			return werrors.NewValidationError("Administrator password cannot contain Chinese characters")
		}
		if unicode.IsSpace(r) {
			return werrors.NewValidationError("Administrator password cannot contain whitespace")
		}
		if r < '!' || r > '~' {
			return werrors.NewValidationError("Administrator password may only contain ASCII letters, numbers and special characters")
		}
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return werrors.NewValidationError("Administrator password must contain both letters and numbers")
	}
	return nil
}

func toAdminTenantView(tenant *types.Tenant) adminTenantView {
	return adminTenantView{
		ID: tenant.ID, Name: tenant.Name, Description: tenant.Description, Status: tenant.Status,
		Business: tenant.Business, StorageQuota: tenant.StorageQuota, StorageUsed: tenant.StorageUsed,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt,
	}
}

func adminActor(c *gin.Context) (*types.User, bool) {
	method, ok := c.Request.Context().Value(types.AuthenticationMethodContextKey).(types.AuthenticationMethod)
	if !ok || method != types.AuthenticationMethodBearer {
		c.Error(werrors.NewForbiddenError("Administrator APIs require user authentication"))
		return nil, false
	}
	actor, ok := types.UserFromContext(c.Request.Context())
	if !ok || actor == nil {
		c.Error(werrors.NewUnauthorizedError("Authentication required"))
		return nil, false
	}
	return actor, true
}

func platformActor(c *gin.Context) (*types.User, bool) {
	actor, ok := adminActor(c)
	if !ok {
		return nil, false
	}
	if !actor.IsPlatformAdmin() {
		c.Error(werrors.NewForbiddenError("Platform administrator permission required"))
		return nil, false
	}
	return actor, true
}

func parseAdminTenantID(c *gin.Context) (uint64, bool) {
	tenantID, err := strconv.ParseUint(c.Param("tenant_id"), 10, 64)
	if err != nil || tenantID == 0 {
		c.Error(werrors.NewBadRequestError("Invalid tenant ID"))
		return 0, false
	}
	return tenantID, true
}

func parseAdminPagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func tenantHasBusinessUsers(users []*types.User, total int64) bool {
	if total > int64(len(users)) {
		return true
	}
	for _, user := range users {
		if user.Role != types.UserRoleTenantAdmin {
			return true
		}
	}
	return false
}

func (h *AdminHandler) ListTenants(c *gin.Context) {
	actor, ok := platformActor(c)
	if !ok {
		return
	}
	page, pageSize := parseAdminPagination(c)
	items, total, err := h.tenantService.SearchTenants(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), 0, actor.TenantID, page, pageSize)
	if err != nil {
		c.Error(werrors.NewInternalServerError("Failed to list tenants").WithDetails(err.Error()))
		return
	}
	views := make([]adminTenantView, 0, len(items))
	for _, item := range items {
		view := toAdminTenantView(item)
		users, userCount, countErr := h.userService.ListTenantUsers(c.Request.Context(), actor, item.ID, "", 0, 100)
		view.CanDelete = countErr == nil && !tenantHasBusinessUsers(users, userCount) && item.StorageUsed == 0 && actor.TenantID != item.ID
		for _, user := range users {
			if user.Role == types.UserRoleTenantAdmin {
				view.AdminUsername = user.Username
				break
			}
		}
		views = append(views, view)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": views, "total": total, "page": page, "page_size": pageSize}})
}

func (h *AdminHandler) CreateTenant(c *gin.Context) {
	actor, ok := platformActor(c)
	if !ok {
		return
	}
	var request adminTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Invalid tenant data").WithDetails(err.Error()))
		return
	}
	if err := validateAdminTenantPassword(request.AdminPassword); err != nil {
		c.Error(err)
		return
	}
	tenant, err := h.tenantService.CreateTenant(c.Request.Context(), &types.Tenant{
		Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
		Business: strings.TrimSpace(request.Business), StorageQuota: request.StorageQuota,
	})
	if err != nil {
		c.Error(werrors.NewInternalServerError("Failed to create tenant").WithDetails(err.Error()))
		return
	}
	password := request.AdminPassword
	if password == "" {
		password = defaultTenantAdminPassword
	}
	admin, err := h.userService.SetTenantAdminCredentials(c.Request.Context(), actor, tenant.ID, request.AdminUsername, password)
	if err != nil {
		_ = h.tenantService.DeleteTenant(c.Request.Context(), tenant.ID)
		c.Error(werrors.NewInternalServerError("Failed to create the initial tenant administrator").WithDetails(err.Error()))
		return
	}
	view := toAdminTenantView(tenant)
	view.AdminUsername = admin.Username
	view.CanDelete = actor.TenantID != tenant.ID
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": gin.H{
		"tenant":        view,
		"initial_admin": gin.H{"username": admin.Username, "password": password},
	}})
}

func (h *AdminHandler) UpdateTenant(c *gin.Context) {
	actor, ok := platformActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request adminTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Invalid tenant data").WithDetails(err.Error()))
		return
	}
	if err := validateAdminTenantPassword(request.AdminPassword); err != nil {
		c.Error(err)
		return
	}
	tenant, err := h.tenantService.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil || tenant == nil {
		c.Error(werrors.NewTenantNotFoundError())
		return
	}
	tenant.Name = strings.TrimSpace(request.Name)
	tenant.Description = strings.TrimSpace(request.Description)
	tenant.Business = strings.TrimSpace(request.Business)
	tenant.StorageQuota = request.StorageQuota
	tenant, err = h.tenantService.UpdateTenant(c.Request.Context(), tenant)
	if err != nil {
		c.Error(werrors.NewInternalServerError("Failed to update tenant").WithDetails(err.Error()))
		return
	}
	admin, err := h.userService.SetTenantAdminCredentials(c.Request.Context(), actor, tenantID, request.AdminUsername, request.AdminPassword)
	if err != nil {
		c.Error(err)
		return
	}
	view := toAdminTenantView(tenant)
	view.AdminUsername = admin.Username
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

func (h *AdminHandler) UpdateTenantStatus(c *gin.Context) {
	actor, ok := platformActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	if actor.TenantID == tenantID {
		c.Error(werrors.NewConflictError("The current platform administrator tenant cannot be suspended"))
		return
	}
	var request adminTenantStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || !types.IsTenantStatus(string(request.Status)) {
		c.Error(werrors.NewValidationError("Status must be active or suspended"))
		return
	}
	tenant, err := h.tenantService.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil || tenant == nil {
		c.Error(werrors.NewTenantNotFoundError())
		return
	}
	tenant.Status = string(request.Status)
	tenant, err = h.tenantService.UpdateTenant(c.Request.Context(), tenant)
	if err != nil {
		c.Error(werrors.NewInternalServerError("Failed to update tenant status").WithDetails(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toAdminTenantView(tenant)})
}

func (h *AdminHandler) DeleteTenant(c *gin.Context) {
	actor, ok := platformActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	if actor.TenantID == tenantID {
		c.Error(werrors.NewConflictError("The current platform administrator tenant cannot be deleted"))
		return
	}
	tenant, err := h.tenantService.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil || tenant == nil {
		c.Error(werrors.NewTenantNotFoundError())
		return
	}
	users, userCount, err := h.userService.ListTenantUsers(c.Request.Context(), actor, tenantID, "", 0, 100)
	if err != nil {
		c.Error(err)
		return
	}
	if tenantHasBusinessUsers(users, userCount) || tenant.StorageUsed > 0 {
		c.Error(werrors.NewConflictError("Tenant is already in use and cannot be deleted; suspend it instead"))
		return
	}
	if err := h.tenantService.DeleteTenant(c.Request.Context(), tenantID); err != nil {
		c.Error(werrors.NewInternalServerError("Failed to delete tenant").WithDetails(err.Error()))
		return
	}
	for _, user := range users {
		if err := h.userService.DeleteUser(c.Request.Context(), user.ID); err != nil {
			c.Error(werrors.NewInternalServerError("Tenant was deleted but its administrator account could not be removed").WithDetails(err.Error()))
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) ListTenantUsers(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	page, pageSize := parseAdminPagination(c)
	users, total, err := h.userService.ListTenantUsers(c.Request.Context(), actor, tenantID, c.Query("keyword"), (page-1)*pageSize, pageSize)
	if err != nil {
		c.Error(err)
		return
	}
	items := make([]*types.UserInfo, 0, len(users))
	for _, user := range users {
		item := user.ToUserInfo()
		canDelete, deleteErr := h.userService.CanDeleteTenantUser(c.Request.Context(), actor, tenantID, user.ID)
		if deleteErr != nil {
			logger.Warnf(c.Request.Context(), "Failed to evaluate delete eligibility for user %s: %v", user.ID, deleteErr)
			item.CanDelete = false
		} else {
			item.CanDelete = canDelete
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items, "total": total, "page": page, "page_size": pageSize}})
}

func (h *AdminHandler) CreateTenantUser(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request types.CreateTenantUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Invalid user data").WithDetails(err.Error()))
		return
	}
	user, err := h.userService.CreateTenantUser(c.Request.Context(), actor, tenantID, &request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": user.ToUserInfo()})
}

func (h *AdminHandler) UpdateTenantUser(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request types.UpdateTenantUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Invalid user data").WithDetails(err.Error()))
		return
	}
	user, err := h.userService.UpdateTenantUser(c.Request.Context(), actor, tenantID, c.Param("user_id"), &request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": user.ToUserInfo()})
}

func (h *AdminHandler) DeleteTenantUser(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	if err := h.userService.DeleteTenantUser(c.Request.Context(), actor, tenantID, c.Param("user_id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) ListTenantKnowledgeBases(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	if !actor.CanManageTenant() || (!actor.IsPlatformAdmin() && actor.TenantID != tenantID) {
		c.Error(werrors.NewForbiddenError("Cross-tenant user management is forbidden"))
		return
	}
	kbs, err := h.kbService.ListKnowledgeBasesByTenantID(c.Request.Context(), tenantID)
	if err != nil {
		c.Error(werrors.NewInternalServerError("Failed to list knowledge bases").WithDetails(err.Error()))
		return
	}
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := make([]option, 0, len(kbs))
	for _, kb := range kbs {
		if kb != nil {
			items = append(items, option{ID: kb.ID, Name: kb.Name})
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *AdminHandler) ResetTenantUserPassword(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request types.ResetTenantUserPasswordRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Password must be 8 to 72 characters").WithDetails(err.Error()))
		return
	}
	if err := h.userService.ResetTenantUserPassword(c.Request.Context(), actor, tenantID, c.Param("user_id"), request.Password); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) UpdateTenantUserRole(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request types.UpdateTenantUserRoleRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(werrors.NewValidationError("Invalid role").WithDetails(err.Error()))
		return
	}
	user, err := h.userService.UpdateTenantUserRole(c.Request.Context(), actor, tenantID, c.Param("user_id"), request.Role)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": user.ToUserInfo()})
}

func (h *AdminHandler) UpdateTenantUserStatus(c *gin.Context) {
	actor, ok := adminActor(c)
	if !ok {
		return
	}
	tenantID, ok := parseAdminTenantID(c)
	if !ok {
		return
	}
	var request types.UpdateTenantUserStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.IsActive == nil {
		c.Error(werrors.NewValidationError("is_active is required"))
		return
	}
	user, err := h.userService.UpdateTenantUserStatus(c.Request.Context(), actor, tenantID, c.Param("user_id"), *request.IsActive)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": user.ToUserInfo()})
}
