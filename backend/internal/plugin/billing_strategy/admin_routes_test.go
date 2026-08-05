package billing_strategy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateAccountMultipliersRoutePersistsPluginConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	router := gin.New()
	RegisterAdminRoutes(router.Group("/admin"))

	request := httptest.NewRequest(http.MethodPut, "/admin/billing-strategy/account-multipliers", bytes.NewBufferString(`{"account_ids":[42,43],"multiplier":20}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 20.0, AccountRateMultiplierFactor(42))
}

func TestUpdateAccountMultipliersRouteRejectsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	router := gin.New()
	RegisterAdminRoutes(router.Group("/admin"))

	request := httptest.NewRequest(http.MethodPut, "/admin/billing-strategy/account-multipliers", bytes.NewBufferString(`{"account_ids":[42],"multiplier":0}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
}
