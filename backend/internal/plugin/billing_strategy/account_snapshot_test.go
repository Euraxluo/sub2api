package billing_strategy

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRememberAccountSnapshotAdminRequestCachesReusableAdminAPIKey(t *testing.T) {
	resetAccountSnapshotHTTPStateForTest(t)
	t.Setenv(accountSnapshotBaseURLEnv, "")
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "")

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "admin-cached", request.Header.Get(accountSnapshotAdminAPIKeyHeader))
		_, _ = response.Write([]byte(`{"code":0,"message":"success","data":{"id":42,"rate_multiplier":0.04}}`))
	}))
	defer server.Close()

	request := httptest.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/billing-strategy/config", nil)
	request.Header.Set(accountSnapshotAdminAPIKeyHeader, "admin-cached")
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request
	rememberAccountSnapshotAdminRequest(ginContext)

	snapshot, err := fetchAccountSnapshot(ginContext.Request.Context(), 42)
	require.NoError(t, err)
	require.Equal(t, AccountSnapshot{AccountID: 42, RateMultiplier: 0.04}, snapshot)
}

func TestRememberAccountSnapshotAdminRequestDoesNotReplaySessionBoundJWT(t *testing.T) {
	resetAccountSnapshotHTTPStateForTest(t)
	t.Setenv(accountSnapshotBaseURLEnv, "")
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "")

	token := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"bnd":"session-binding"}`)) + ".signature"
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/admin/billing-strategy/config", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request
	rememberAccountSnapshotAdminRequest(ginContext)

	_, _, _, err := resolveAccountSnapshotHTTPConfig()
	require.ErrorIs(t, err, ErrAccountSnapshotAuthMissing)
}

func TestRememberAccountSnapshotAdminRequestReusesUnboundAdminJWT(t *testing.T) {
	resetAccountSnapshotHTTPStateForTest(t)
	t.Setenv(accountSnapshotBaseURLEnv, "")
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "")

	token := "e30." + base64.RawURLEncoding.EncodeToString([]byte(`{"role":"admin"}`)) + ".signature"
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/v1/admin/billing-strategy/config", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = request
	rememberAccountSnapshotAdminRequest(ginContext)

	_, headerName, headerValue, err := resolveAccountSnapshotHTTPConfig()
	require.NoError(t, err)
	require.Equal(t, "Authorization", headerName)
	require.Equal(t, "Bearer "+token, headerValue)
}

func TestFetchAccountSnapshotRejectsMissingRateMultiplier(t *testing.T) {
	resetAccountSnapshotHTTPStateForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"code":0,"message":"success","data":{"id":42}}`))
	}))
	defer server.Close()
	t.Setenv(accountSnapshotBaseURLEnv, server.URL)
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "admin-test")

	_, err := fetchAccountSnapshot(t.Context(), 42)
	require.ErrorIs(t, err, ErrAccountSnapshotInvalid)
	require.ErrorContains(t, err, "rate_multiplier is missing")
}

func resetAccountSnapshotHTTPStateForTest(t *testing.T) {
	t.Helper()
	accountSnapshotHTTPState.Lock()
	previous := accountSnapshotHTTPState.config
	accountSnapshotHTTPState.config = accountSnapshotHTTPConfig{}
	accountSnapshotHTTPState.Unlock()
	t.Cleanup(func() {
		accountSnapshotHTTPState.Lock()
		accountSnapshotHTTPState.config = previous
		accountSnapshotHTTPState.Unlock()
	})
}
