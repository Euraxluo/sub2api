package admin_jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	builtinSub2APIBalance = "sub2api_upstream_balance"
	sub2APIJobUserAgent   = "Go-http-client/1.1"
)

func init() {
	RegisterBuiltin(BuiltinDefinition{
		ID:          builtinSub2APIBalance,
		Name:        "Sub2API 上游余额监控",
		Description: "使用 API Key、Refresh Token 或账号密码读取上游资料中的余额，并在低于阈值时触发通知。",
		Fields: []FieldDefinition{
			{Key: "target_name", Label: "上游名称", Type: "text", Default: "Sub2API 上游", Placeholder: "例如：香港上游"},
			{Key: "base_url", Label: "上游地址", Type: "url", Required: true, Placeholder: "https://upstream.example.com"},
			{Key: "login_email", Label: "登录账号", Type: "text", Placeholder: "name@example.com", Help: "与登录密码同时填写后启用登录模式，并覆盖已保存的 API Key / Access Token。首次登录后会自动保存上游返回的 Refresh Token。"},
			{Key: "login_password", Label: "登录密码", Type: "password", Secret: true, Help: "仅用于从运行该任务的容器登录上游；不显示在任务列表或执行记录中。"},
			{Key: "refresh_token", Label: "Refresh Token", Type: "password", Secret: true, Help: "可单独填写；账号密码登录后也会自动保存并轮换。"},
			{Key: "api_key", Label: "API Key / Access Token", Type: "password", Secret: true, Help: "未启用账号密码登录时，填写后直接作为 Bearer Token 使用。"},
			{Key: "threshold", Label: "余额阈值", Type: "number", Default: 10.0},
			{Key: "login_path", Label: "登录接口", Type: "text", Default: "/api/v1/auth/login"},
			{Key: "refresh_path", Label: "刷新接口", Type: "text", Default: "/api/v1/auth/refresh"},
			{Key: "profile_path", Label: "用户资料接口", Type: "text", Default: "/api/v1/user/profile"},
			{Key: "balance_path", Label: "余额 JSON 路径", Type: "text", Default: "data.balance", Help: "兼容 gjson 路径；缺失时自动尝试 balance 和 data.quota。"},
			{Key: "currency_path", Label: "币种 JSON 路径", Type: "text", Default: "data.currency"},
		},
	}, runSub2APIUpstreamBalance)
}

func runSub2APIUpstreamBalance(ctx context.Context, payload RunPayload) (Result, error) {
	var result Result
	baseURL := strings.TrimRight(inputString(payload.Input, "base_url", ""), "/")
	parsedURL, err := url.Parse(baseURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return Result{}, fmt.Errorf("invalid Sub2API base_url")
	}
	targetName := inputString(payload.Input, "target_name", "Sub2API 上游")
	threshold := inputFloat(payload.Input, "threshold", 10)
	if threshold < 0 {
		return Result{}, fmt.Errorf("threshold must be non-negative")
	}

	loginEmail := strings.TrimSpace(inputString(payload.Input, "login_email", ""))
	loginPassword := payload.Secrets["login_password"]
	passwordLoginEnabled := loginEmail != "" && strings.TrimSpace(loginPassword) != ""
	token := ""
	if !passwordLoginEnabled {
		token = strings.TrimSpace(payload.Secrets["api_key"])
	}
	if token == "" {
		refreshToken := strings.TrimSpace(payload.Secrets["refresh_token"])
		var updatedRefreshToken string

		if refreshToken != "" {
			refreshPath := normalizeEndpointPath(inputString(payload.Input, "refresh_path", "/api/v1/auth/refresh"))
			token, updatedRefreshToken, err = refreshSub2APIToken(ctx, baseURL+refreshPath, refreshToken)
			if err != nil {
				if loginEmail == "" || strings.TrimSpace(loginPassword) == "" || !isSub2APIAuthenticationFailure(err) {
					return result, fmt.Errorf("refresh upstream token: %w", err)
				}
				token, updatedRefreshToken, err = loginSub2API(ctx, baseURL+normalizeEndpointPath(inputString(payload.Input, "login_path", "/api/v1/auth/login")), loginEmail, loginPassword)
				if err != nil {
					return result, fmt.Errorf("login upstream account after refresh failed: %w", err)
				}
			}
		} else {
			if loginEmail == "" || strings.TrimSpace(loginPassword) == "" {
				return Result{}, fmt.Errorf("refresh_token, api_key, or login_email and login_password are required")
			}
			token, updatedRefreshToken, err = loginSub2API(ctx, baseURL+normalizeEndpointPath(inputString(payload.Input, "login_path", "/api/v1/auth/login")), loginEmail, loginPassword)
			if err != nil {
				return result, fmt.Errorf("login upstream account: %w", err)
			}
		}

		if updatedRefreshToken != "" && updatedRefreshToken != refreshToken {
			result.secretUpdates = map[string]string{"refresh_token": updatedRefreshToken}
		}
	}

	profilePath := normalizeEndpointPath(inputString(payload.Input, "profile_path", "/api/v1/user/profile"))
	body, err := sub2APIRequest(ctx, http.MethodGet, baseURL+profilePath, token, nil)
	if err != nil {
		return result, fmt.Errorf("fetch upstream profile: %w", err)
	}
	balancePath := inputString(payload.Input, "balance_path", "data.balance")
	balance, found := firstJSONNumber(body, balancePath, "data.balance", "balance", "data.quota", "quota")
	if !found {
		return result, fmt.Errorf("balance field was not found in upstream profile")
	}
	currencyPath := inputString(payload.Input, "currency_path", "data.currency")
	currency := firstJSONString(body, currencyPath, "data.currency", "currency")
	if currency == "" {
		currency = "USD"
	}

	result.Status = StatusOK
	result.Title = targetName + "余额正常"
	result.Message = fmt.Sprintf("当前余额 %.4f %s，阈值 %.4f %s", balance, currency, threshold, currency)
	result.DedupKey = "sub2api-upstream-balance:" + strings.ToLower(parsedURL.Host)
	result.Data = map[string]any{
		"target_name": targetName, "base_url": baseURL,
		"balance": balance, "currency": currency, "threshold": threshold,
		"observed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if balance < threshold {
		result.Status = StatusWarning
		result.Title = targetName + "余额不足"
	}
	return result, nil
}

func refreshSub2APIToken(ctx context.Context, endpoint, refreshToken string) (string, string, error) {
	payload, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	body, err := sub2APIRequest(ctx, http.MethodPost, endpoint, "", payload)
	if err != nil {
		return "", "", err
	}
	accessToken := accessTokenFromResponse(body)
	if accessToken == "" {
		return "", "", fmt.Errorf("upstream refresh response did not contain an access token")
	}
	rotatedRefreshToken := firstJSONString(body, "data.refresh_token", "refresh_token")
	return accessToken, rotatedRefreshToken, nil
}

func loginSub2API(ctx context.Context, endpoint, email, password string) (string, string, error) {
	payload, _ := json.Marshal(map[string]string{"email": email, "password": password})
	body, err := sub2APIRequest(ctx, http.MethodPost, endpoint, "", payload)
	if err != nil {
		return "", "", err
	}
	if gjson.GetBytes(body, "data.requires_2fa").Bool() || gjson.GetBytes(body, "requires_2fa").Bool() {
		return "", "", fmt.Errorf("upstream login requires 2FA; use a dedicated monitoring credential instead")
	}
	accessToken := accessTokenFromResponse(body)
	if accessToken == "" {
		return "", "", fmt.Errorf("upstream login response did not contain an access token")
	}
	return accessToken, firstJSONString(body, "data.refresh_token", "refresh_token"), nil
}

func accessTokenFromResponse(body []byte) string {
	return firstJSONString(body,
		"data.access_token", "data.auth_token", "data.token",
		"access_token", "auth_token", "token",
	)
}

func isSub2APIAuthenticationFailure(err error) bool {
	var upstreamErr *sub2APIHTTPError
	return errors.As(err, &upstreamErr) && (upstreamErr.StatusCode == http.StatusUnauthorized || upstreamErr.StatusCode == http.StatusForbidden)
}

type sub2APIHTTPError struct {
	StatusCode int
	Body       string
}

func (e *sub2APIHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func sub2APIRequest(ctx context.Context, method, endpoint, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", sub2APIJobUserAgent)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 256*1024 {
		return nil, fmt.Errorf("upstream response is too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &sub2APIHTTPError{StatusCode: resp.StatusCode, Body: tailText(strings.TrimSpace(string(data)), 500)}
	}
	return data, nil
}

func normalizeEndpointPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func inputString(input map[string]any, key, fallback string) string {
	if value, ok := input[key]; ok {
		if normalized := strings.TrimSpace(fmt.Sprint(value)); normalized != "" {
			return normalized
		}
	}
	return fallback
}

func inputFloat(input map[string]any, key string, fallback float64) float64 {
	value, ok := input[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func firstJSONNumber(body []byte, paths ...string) (float64, bool) {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result := gjson.GetBytes(body, path)
		if !result.Exists() {
			continue
		}
		if result.Type == gjson.Number {
			return result.Float(), true
		}
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(result.String()), 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func firstJSONString(body []byte, paths ...string) string {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}
