package model_reasoning_effort

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateConfigBatchWritesSelectedAccountsWithoutChangingOthers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	require.NoError(t, SaveConfig(Config{Accounts: map[string]AccountConfig{
		"9": {Mappings: []Mapping{{FromModel: "gpt-5.4", FromEffort: "medium", ToModel: "gpt-5.4", ToEffort: "low"}}},
	}}))

	router := gin.New()
	group := router.Group("/admin")
	RegisterAdminRoutes(group)
	body := []byte(`{"account_ids":[3,2,3],"config":{"mappings":[{"from_model":"gpt-5.6-sol","from_effort":"xhigh","to_model":"gpt-5.5","to_effort":"xhigh"}]}}`)
	req := httptest.NewRequest(http.MethodPut, "/admin/model-reasoning-effort/config/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	config, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", config.Accounts["2"].Mappings[0].FromModel)
	require.Equal(t, "gpt-5.6-sol", config.Accounts["3"].Mappings[0].FromModel)
	require.Equal(t, "gpt-5.4", config.Accounts["9"].Mappings[0].FromModel)
}

func TestUpdateConfigBatchRejectsInvalidAccountIDsBeforeLoadingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	router := gin.New()
	group := router.Group("/admin")
	RegisterAdminRoutes(group)
	req := httptest.NewRequest(http.MethodPut, "/admin/model-reasoning-effort/config/batch", bytes.NewReader([]byte(`{"account_ids":[1,0],"config":{"mappings":[]}}`)))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusBadRequest, response.Code)
}

func TestClearAllConfigsRemovesMappingsAndDisablesAutomaticRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	require.NoError(t, SaveConfig(Config{
		Accounts: map[string]AccountConfig{
			"2": {Mappings: []Mapping{{FromModel: "gpt-5.6-sol", FromEffort: "max", ToModel: "gpt-5.6-luna", ToEffort: "max"}}},
			"3": {Mappings: []Mapping{{FromModel: "gpt-5.6-terra", FromEffort: "ultra", ToModel: "gpt-5.6-sol", ToEffort: "xhigh"}}},
		},
		Auto: AutoRoutingConfig{
			Enabled:           true,
			UnavailableModels: []string{"gpt-5.6-luna"},
		},
	}))

	router := gin.New()
	group := router.Group("/admin")
	RegisterAdminRoutes(group)
	req := httptest.NewRequest(http.MethodDelete, "/admin/model-reasoning-effort/config", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code)
	config, err := LoadConfig()
	require.NoError(t, err)
	require.Empty(t, config.Accounts)
	require.False(t, config.Auto.Enabled)
	require.Equal(t, []string{"gpt-5.6-luna"}, config.Auto.UnavailableModels)
}
