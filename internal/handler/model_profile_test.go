package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type modelProfileTenantService struct {
	interfaces.TenantService
	settings *types.PlatformSettings
}

func (s *modelProfileTenantService) GetPlatformSettings(context.Context) (*types.PlatformSettings, error) {
	return s.settings, nil
}

func (s *modelProfileTenantService) UpdatePlatformSettings(_ context.Context, settings *types.PlatformSettings) (*types.PlatformSettings, error) {
	s.settings = settings
	return settings, nil
}

func TestUpdateModelProfileAllowsEmptyTargetProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tenantSvc := &modelProfileTenantService{settings: &types.PlatformSettings{ID: 1, ModelProfile: types.ModelProfileOnline}}
	h := &SystemHandler{tenantSvc: tenantSvc}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/system/model-profile", bytes.NewBufferString(`{"profile":"offline"}`))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req.WithContext(context.WithValue(req.Context(), types.UserContextKey, &types.User{Role: types.UserRolePlatformAdmin}))

	h.UpdateModelProfile(c)

	if len(c.Errors) != 0 {
		t.Fatalf("unexpected profile switch error: %v", c.Errors)
	}
	if w.Code != http.StatusOK || tenantSvc.settings.ModelProfile != types.ModelProfileOffline {
		t.Fatalf("profile switch failed: status=%d profile=%q", w.Code, tenantSvc.settings.ModelProfile)
	}
}
