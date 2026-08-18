package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type missingTenantService struct {
	interfaces.TenantService
}

func (missingTenantService) ExtractTenantIDFromAPIKey(string) (uint64, error) {
	return 42, nil
}

func (missingTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return nil, nil
}

func TestAuthRejectsAPIKeyWhenTenantLookupReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Auth(missingTenantService{}, nil, nil))
	router.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("X-API-Key", "tenant-42-key")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
