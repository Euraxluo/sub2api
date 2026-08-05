package admin_jobs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunSub2APIUpstreamBalanceRefreshesTokenAndWarns(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "rt-test", body["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"access_token": "access-test", "refresh_token": "rt-next",
		}})
	})
	mux.HandleFunc("/api/v1/user/profile", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-test", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"balance": 3.25, "currency": "USD"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	result, err := runSub2APIUpstreamBalance(context.Background(), RunPayload{
		TaskID: "task-1", TaskName: "余额监控",
		Input:   map[string]any{"base_url": server.URL, "target_name": "测试上游", "threshold": 10.0},
		Secrets: map[string]string{"refresh_token": "rt-test"},
	})
	require.NoError(t, err)
	require.Equal(t, StatusWarning, result.Status)
	require.Equal(t, "测试上游余额不足", result.Title)
	require.Equal(t, 3.25, result.Data["balance"])
	require.Equal(t, 10.0, result.Data["threshold"])
	require.Equal(t, "rt-next", result.secretUpdates["refresh_token"])
}

func TestRunSub2APIUpstreamBalanceAcceptsZeroBalanceWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/user/profile", r.URL.Path)
		require.Equal(t, "Bearer api-test", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"balance": 0, "currency": "CNY"}})
	}))
	defer server.Close()

	result, err := runSub2APIUpstreamBalance(context.Background(), RunPayload{
		Input:   map[string]any{"base_url": server.URL, "threshold": 1.0},
		Secrets: map[string]string{"api_key": "api-test"},
	})
	require.NoError(t, err)
	require.Equal(t, StatusWarning, result.Status)
	require.Equal(t, float64(0), result.Data["balance"])
	require.Equal(t, "CNY", result.Data["currency"])
}
