package model_reasoning_effort

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const (
	quotaGuardPageSize             = 500
	quotaGuardDefaultIntervalSecs  = 60
	quotaGuardHeaderAPIKey         = "x-api-key"
	quotaGuardSourceProvided       = "provided"
	quotaGuardSourceAutoCreated    = "auto_created"
	quotaGuardSourceMissing        = "missing"
	quotaGuardDefaultSpendTimezone = "Asia/Shanghai"
	quotaGuardMaxAccountIDs        = 500
	quotaGuardMaxPolicies          = 100
)

var globalQuotaGuard = newQuotaGuardManager()

type quotaGuardStartRequest struct {
	Policies            []quotaGuardPolicyRequest `json:"policies"`
	Enabled             *bool                     `json:"enabled"`
	IntervalSeconds     *int                      `json:"interval_seconds"`
	AccountIDs          []int64                   `json:"account_ids"`
	DailySpendLimitUSD  *float64                  `json:"daily_spend_limit_usd"`
	DailyTokenLimit     *int64                    `json:"daily_token_limit"`
	WeeklySpendLimitUSD *float64                  `json:"weekly_spend_limit_usd"`
	WeeklyTokenLimit    *int64                    `json:"weekly_token_limit"`
	DailySpendTimezone  string                    `json:"daily_spend_timezone"`
	DryRun              bool                      `json:"dry_run"`
}

type quotaGuardPolicyRequest struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Enabled             *bool    `json:"enabled"`
	IntervalSeconds     *int     `json:"interval_seconds"`
	AccountIDs          []int64  `json:"account_ids"`
	DailySpendLimitUSD  *float64 `json:"daily_spend_limit_usd"`
	DailyTokenLimit     *int64   `json:"daily_token_limit"`
	WeeklySpendLimitUSD *float64 `json:"weekly_spend_limit_usd"`
	WeeklyTokenLimit    *int64   `json:"weekly_token_limit"`
	DailySpendTimezone  string   `json:"daily_spend_timezone"`
	DryRun              bool     `json:"dry_run"`
}

type quotaGuardConfig struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Enabled             bool    `json:"enabled"`
	IntervalSeconds     int     `json:"interval_seconds"`
	AccountIDs          []int64 `json:"account_ids,omitempty"`
	DailySpendLimitUSD  float64 `json:"daily_spend_limit_usd"`
	DailyTokenLimit     int64   `json:"daily_token_limit"`
	WeeklySpendLimitUSD float64 `json:"weekly_spend_limit_usd"`
	WeeklyTokenLimit    int64   `json:"weekly_token_limit"`
	DailySpendTimezone  string  `json:"daily_spend_timezone"`
	DryRun              bool    `json:"dry_run"`
}

type quotaGuardStatusResponse struct {
	Running               bool               `json:"running"`
	Config                quotaGuardConfig   `json:"config"`
	Policies              []quotaGuardConfig `json:"policies,omitempty"`
	AdminAPIKeySource     string             `json:"admin_api_key_source"`
	AdminAPIKeyCached     bool               `json:"admin_api_key_cached"`
	LastRunAt             string             `json:"last_run_at,omitempty"`
	LastError             string             `json:"last_error,omitempty"`
	LastBlockedCount      int                `json:"last_blocked_count"`
	LastBlockedIDs        []int64            `json:"last_blocked_ids,omitempty"`
	LastReleasedCount     int                `json:"last_released_count"`
	LastReleasedIDs       []int64            `json:"last_released_ids,omitempty"`
	LastScanCandidates    int                `json:"last_scan_candidates"`
	LastScannedAccounts   int                `json:"last_scanned_accounts"`
	CurrentManagedCount   int                `json:"current_managed_count"`
	CurrentManagedIDs     []int64            `json:"current_managed_ids,omitempty"`
	DailySpendByAccount   map[int64]float64  `json:"daily_spend_by_account,omitempty"`
	DailyTokensByAccount  map[int64]int64    `json:"daily_tokens_by_account,omitempty"`
	WeeklySpendByAccount  map[int64]float64  `json:"weekly_spend_by_account,omitempty"`
	WeeklyTokensByAccount map[int64]int64    `json:"weekly_tokens_by_account,omitempty"`
}

type quotaGuardScanResponse struct {
	BlockedCount          int               `json:"blocked_count"`
	BlockedIDs            []int64           `json:"blocked_ids,omitempty"`
	ReleasedCount         int               `json:"released_count"`
	ReleasedIDs           []int64           `json:"released_ids,omitempty"`
	CandidateCount        int               `json:"candidate_count"`
	ScannedAccounts       int               `json:"scanned_accounts"`
	DryRun                bool              `json:"dry_run"`
	Errors                []string          `json:"errors,omitempty"`
	StoppedOnAuthErr      bool              `json:"stopped_on_auth_error"`
	DailySpendByAccount   map[int64]float64 `json:"daily_spend_by_account,omitempty"`
	DailyTokensByAccount  map[int64]int64   `json:"daily_tokens_by_account,omitempty"`
	WeeklySpendByAccount  map[int64]float64 `json:"weekly_spend_by_account,omitempty"`
	WeeklyTokensByAccount map[int64]int64   `json:"weekly_tokens_by_account,omitempty"`
}

type quotaGuardManager struct {
	mu sync.Mutex

	running               bool
	config                quotaGuardConfig
	policies              []quotaGuardConfig
	adminAPIKey           string
	adminAPIKeySource     string
	baseURL               string
	lastRunAt             time.Time
	lastError             string
	lastBlockedCount      int
	lastBlockedIDs        []int64
	lastReleasedCount     int
	lastReleasedIDs       []int64
	lastScanCandidates    int
	lastScannedAccounts   int
	currentManagedCount   int
	currentManagedIDs     []int64
	dailySpendByAccount   map[int64]float64
	dailyTokensByAccount  map[int64]int64
	weeklySpendByAccount  map[int64]float64
	weeklyTokensByAccount map[int64]int64
	stopCh                chan struct{}
}

type quotaGuardAccountsEnvelope struct {
	Data struct {
		Items []quotaGuardAccount `json:"items"`
		Total int64               `json:"total"`
		Page  int                 `json:"page"`
		Pages int                 `json:"pages"`
	} `json:"data"`
	Message string `json:"message"`
}

type quotaGuardAccount struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Schedulable bool           `json:"schedulable"`
	Extra       map[string]any `json:"extra"`
}

type quotaGuardBulkUpdatePayload struct {
	AccountIDs  []int64        `json:"account_ids"`
	Schedulable *bool          `json:"schedulable,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

type quotaGuardAdminAPIKeyStatusEnvelope struct {
	Data struct {
		Exists bool `json:"exists"`
	} `json:"data"`
}

type quotaGuardAdminAPIKeyRegenerateEnvelope struct {
	Data struct {
		Key string `json:"key"`
	} `json:"data"`
}

func registerQuotaGuardRoutes(adminGroup *gin.RouterGroup) {
	adminGroup.POST("/codex-quota-guard/start", quotaGuardStart)
	adminGroup.POST("/codex-quota-guard/stop", quotaGuardStop)
	adminGroup.GET("/codex-quota-guard/status", quotaGuardStatus)
	adminGroup.POST("/codex-quota-guard/scan", quotaGuardScan)
	adminGroup.POST("/codex-quota-guard/release", quotaGuardRelease)
}

func newQuotaGuardManager() *quotaGuardManager {
	return &quotaGuardManager{
		config: quotaGuardConfig{
			ID:                 "default",
			Name:               "默认限制器",
			Enabled:            true,
			IntervalSeconds:    quotaGuardDefaultIntervalSecs,
			DailySpendTimezone: quotaGuardDefaultSpendTimezone,
		},
		adminAPIKeySource: quotaGuardSourceMissing,
	}
}

func quotaGuardStart(c *gin.Context) {
	var req quotaGuardStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	policies, err := normalizeQuotaGuardPolicies(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	apiKey, source, err := globalQuotaGuard.resolveAdminAPIKey(c, policies)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, globalQuotaGuard.start(apiKey, source, buildQuotaGuardBaseURL(c), policies))
}

func quotaGuardStop(c *gin.Context) {
	response.Success(c, globalQuotaGuard.stop())
}

func quotaGuardStatus(c *gin.Context) {
	response.Success(c, globalQuotaGuard.status())
}

func quotaGuardScan(c *gin.Context) {
	result := globalQuotaGuard.runScan(c.Request.Context())
	if result.StoppedOnAuthErr {
		globalQuotaGuard.stop()
	}
	response.Success(c, result)
}

func quotaGuardRelease(c *gin.Context) {
	result := globalQuotaGuard.releaseManaged(c.Request.Context())
	if result.StoppedOnAuthErr {
		globalQuotaGuard.stop()
	}
	response.Success(c, result)
}

func normalizeQuotaGuardConfig(req quotaGuardStartRequest) (quotaGuardConfig, error) {
	policies, err := normalizeQuotaGuardPolicies(req)
	if err != nil {
		return quotaGuardConfig{}, err
	}
	return policies[0], nil
}

func normalizeQuotaGuardPolicies(req quotaGuardStartRequest) ([]quotaGuardConfig, error) {
	if len(req.Policies) == 0 {
		policy := quotaGuardPolicyRequest{
			ID:                  "default",
			Name:                "默认限制器",
			Enabled:             req.Enabled,
			IntervalSeconds:     req.IntervalSeconds,
			AccountIDs:          req.AccountIDs,
			DailySpendLimitUSD:  req.DailySpendLimitUSD,
			DailyTokenLimit:     req.DailyTokenLimit,
			WeeklySpendLimitUSD: req.WeeklySpendLimitUSD,
			WeeklyTokenLimit:    req.WeeklyTokenLimit,
			DailySpendTimezone:  req.DailySpendTimezone,
			DryRun:              req.DryRun,
		}
		req.Policies = []quotaGuardPolicyRequest{policy}
	}
	if len(req.Policies) > quotaGuardMaxPolicies {
		return nil, fmt.Errorf("policies must contain at most %d limiters", quotaGuardMaxPolicies)
	}
	policies := make([]quotaGuardConfig, 0, len(req.Policies))
	seenIDs := make(map[string]struct{}, len(req.Policies))
	for index, policy := range req.Policies {
		cfg := normalizeQuotaGuardPolicyRequest(policy, index+1)
		if err := validateQuotaGuardConfig(cfg); err != nil {
			return nil, fmt.Errorf("policy %q: %w", cfg.ID, err)
		}
		if _, exists := seenIDs[cfg.ID]; exists {
			return nil, fmt.Errorf("duplicate limiter id: %s", cfg.ID)
		}
		seenIDs[cfg.ID] = struct{}{}
		policies = append(policies, cfg)
	}
	return policies, nil
}

func normalizeQuotaGuardPolicyRequest(req quotaGuardPolicyRequest, index int) quotaGuardConfig {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("limiter-%d", index)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("限制器 %d", index)
	}
	cfg := quotaGuardConfig{
		ID:                 id,
		Name:               name,
		Enabled:            true,
		IntervalSeconds:    quotaGuardDefaultIntervalSecs,
		DailySpendTimezone: quotaGuardDefaultSpendTimezone,
		DryRun:             req.DryRun,
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.IntervalSeconds != nil {
		cfg.IntervalSeconds = *req.IntervalSeconds
	}
	cfg.AccountIDs = normalizeQuotaAccountIDs(req.AccountIDs)
	if req.DailySpendLimitUSD != nil {
		cfg.DailySpendLimitUSD = *req.DailySpendLimitUSD
	}
	if req.DailyTokenLimit != nil {
		cfg.DailyTokenLimit = *req.DailyTokenLimit
	}
	if req.WeeklySpendLimitUSD != nil {
		cfg.WeeklySpendLimitUSD = *req.WeeklySpendLimitUSD
	}
	if req.WeeklyTokenLimit != nil {
		cfg.WeeklyTokenLimit = *req.WeeklyTokenLimit
	}
	if timezoneName := strings.TrimSpace(req.DailySpendTimezone); timezoneName != "" {
		cfg.DailySpendTimezone = timezoneName
	}
	return cfg
}

func validateQuotaGuardConfig(cfg quotaGuardConfig) error {
	if math.IsNaN(cfg.DailySpendLimitUSD) || math.IsInf(cfg.DailySpendLimitUSD, 0) || cfg.DailySpendLimitUSD < 0 {
		return errors.New("daily_spend_limit_usd must be finite and >= 0")
	}
	if math.IsNaN(cfg.WeeklySpendLimitUSD) || math.IsInf(cfg.WeeklySpendLimitUSD, 0) || cfg.WeeklySpendLimitUSD < 0 {
		return errors.New("weekly_spend_limit_usd must be finite and >= 0")
	}
	if cfg.DailyTokenLimit < 0 {
		return errors.New("daily_token_limit must be >= 0")
	}
	if cfg.WeeklyTokenLimit < 0 {
		return errors.New("weekly_token_limit must be >= 0")
	}
	if len(cfg.AccountIDs) > quotaGuardMaxAccountIDs {
		return fmt.Errorf("account_ids must contain at most %d accounts", quotaGuardMaxAccountIDs)
	}
	if _, err := time.LoadLocation(cfg.DailySpendTimezone); err != nil {
		return fmt.Errorf("daily_spend_timezone is invalid: %w", err)
	}
	if cfg.IntervalSeconds <= 0 {
		return errors.New("interval_seconds must be > 0")
	}
	return nil
}

func normalizeQuotaAccountIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func cloneQuotaGuardConfigs(values []quotaGuardConfig) []quotaGuardConfig {
	if len(values) == 0 {
		return nil
	}
	result := make([]quotaGuardConfig, len(values))
	for index, value := range values {
		result[index] = value
		result[index].AccountIDs = append([]int64(nil), value.AccountIDs...)
	}
	return result
}

func quotaGuardPoliciesEnabled(policies []quotaGuardConfig) bool {
	for _, policy := range policies {
		if policy.Enabled {
			return true
		}
	}
	return false
}

func quotaGuardPoliciesHaveAnyLimit(policies []quotaGuardConfig) bool {
	for _, policy := range policies {
		if policy.Enabled && quotaGuardHasAnyLimit(policy) {
			return true
		}
	}
	return false
}

func (manager *quotaGuardManager) start(apiKey, source, baseURL string, policies []quotaGuardConfig) quotaGuardStatusResponse {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.stopCh != nil {
		close(manager.stopCh)
	}
	manager.stopCh = make(chan struct{})
	manager.policies = cloneQuotaGuardConfigs(policies)
	manager.config = manager.policies[0]
	manager.running = quotaGuardPoliciesEnabled(manager.policies)
	manager.adminAPIKey = strings.TrimSpace(apiKey)
	manager.adminAPIKeySource = source
	manager.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	manager.lastError = ""
	if manager.running {
		go manager.loop(manager.stopCh)
	}
	return manager.snapshotLocked()
}

func (manager *quotaGuardManager) stop() quotaGuardStatusResponse {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.stopCh != nil {
		close(manager.stopCh)
		manager.stopCh = nil
	}
	manager.running = false
	manager.adminAPIKey = ""
	manager.adminAPIKeySource = quotaGuardSourceMissing
	manager.baseURL = ""
	return manager.snapshotLocked()
}

func (manager *quotaGuardManager) status() quotaGuardStatusResponse {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.snapshotLocked()
}

func (manager *quotaGuardManager) snapshotLocked() quotaGuardStatusResponse {
	result := quotaGuardStatusResponse{
		Running:               manager.running,
		Config:                manager.config,
		Policies:              cloneQuotaGuardConfigs(manager.policies),
		AdminAPIKeySource:     quotaGuardSourceMissing,
		AdminAPIKeyCached:     strings.TrimSpace(manager.adminAPIKey) != "",
		LastError:             manager.lastError,
		LastBlockedCount:      manager.lastBlockedCount,
		LastBlockedIDs:        append([]int64(nil), manager.lastBlockedIDs...),
		LastReleasedCount:     manager.lastReleasedCount,
		LastReleasedIDs:       append([]int64(nil), manager.lastReleasedIDs...),
		LastScanCandidates:    manager.lastScanCandidates,
		LastScannedAccounts:   manager.lastScannedAccounts,
		CurrentManagedCount:   manager.currentManagedCount,
		CurrentManagedIDs:     append([]int64(nil), manager.currentManagedIDs...),
		DailySpendByAccount:   cloneQuotaFloatMap(manager.dailySpendByAccount),
		DailyTokensByAccount:  cloneQuotaIntMap(manager.dailyTokensByAccount),
		WeeklySpendByAccount:  cloneQuotaFloatMap(manager.weeklySpendByAccount),
		WeeklyTokensByAccount: cloneQuotaIntMap(manager.weeklyTokensByAccount),
	}
	if strings.TrimSpace(manager.adminAPIKeySource) != "" {
		result.AdminAPIKeySource = manager.adminAPIKeySource
	}
	if !manager.lastRunAt.IsZero() {
		result.LastRunAt = manager.lastRunAt.UTC().Format(time.RFC3339)
	}
	return result
}

func cloneQuotaFloatMap(values map[int64]float64) map[int64]float64 {
	if len(values) == 0 {
		return nil
	}
	result := make(map[int64]float64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneQuotaIntMap(values map[int64]int64) map[int64]int64 {
	if len(values) == 0 {
		return nil
	}
	result := make(map[int64]int64, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (manager *quotaGuardManager) resolveAdminAPIKey(c *gin.Context, policies []quotaGuardConfig) (string, string, error) {
	if key := strings.TrimSpace(c.GetHeader(quotaGuardHeaderAPIKey)); key != "" {
		return key, quotaGuardSourceProvided, nil
	}
	manager.mu.Lock()
	cachedKey := strings.TrimSpace(manager.adminAPIKey)
	cachedSource := strings.TrimSpace(manager.adminAPIKeySource)
	manager.mu.Unlock()
	if cachedKey != "" {
		if cachedSource == "" {
			cachedSource = quotaGuardSourceProvided
		}
		return cachedKey, cachedSource, nil
	}
	if !quotaGuardPoliciesEnabled(policies) {
		return "", quotaGuardSourceMissing, nil
	}

	statusURL := buildQuotaGuardLocalURL(c, "/api/v1/admin/settings/admin-api-key")
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, statusURL, nil)
	if err != nil {
		return "", quotaGuardSourceMissing, err
	}
	copyQuotaGuardAuthHeaders(c, request)
	client := &http.Client{Timeout: 20 * time.Second}
	responseBody, err := client.Do(request)
	if err != nil {
		return "", quotaGuardSourceMissing, fmt.Errorf("query admin api key status failed: %w", err)
	}
	defer responseBody.Body.Close()
	var status quotaGuardAdminAPIKeyStatusEnvelope
	if err := json.NewDecoder(responseBody.Body).Decode(&status); err != nil {
		return "", quotaGuardSourceMissing, fmt.Errorf("decode admin api key status failed: %w", err)
	}
	if responseBody.StatusCode >= http.StatusBadRequest {
		return "", quotaGuardSourceMissing, fmt.Errorf("query admin api key status failed: HTTP %d", responseBody.StatusCode)
	}
	if status.Data.Exists {
		return "", quotaGuardSourceMissing, errors.New("admin api key already exists; please start with x-api-key header")
	}

	createURL := buildQuotaGuardLocalURL(c, "/api/v1/admin/settings/admin-api-key/regenerate")
	createRequest, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, createURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", quotaGuardSourceMissing, err
	}
	createRequest.Header.Set("Content-Type", "application/json")
	copyQuotaGuardAuthHeaders(c, createRequest)
	createResponse, err := client.Do(createRequest)
	if err != nil {
		return "", quotaGuardSourceMissing, fmt.Errorf("create admin api key failed: %w", err)
	}
	defer createResponse.Body.Close()
	var created quotaGuardAdminAPIKeyRegenerateEnvelope
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		return "", quotaGuardSourceMissing, fmt.Errorf("decode created admin api key failed: %w", err)
	}
	if createResponse.StatusCode >= http.StatusBadRequest {
		return "", quotaGuardSourceMissing, fmt.Errorf("create admin api key failed: HTTP %d", createResponse.StatusCode)
	}
	if strings.TrimSpace(created.Data.Key) == "" {
		return "", quotaGuardSourceMissing, errors.New("created admin api key is empty")
	}
	return strings.TrimSpace(created.Data.Key), quotaGuardSourceAutoCreated, nil
}

func (manager *quotaGuardManager) loop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(manager.currentIntervalSeconds()) * time.Second)
	defer ticker.Stop()
	for {
		result := manager.runScan(context.Background())
		if result.StoppedOnAuthErr {
			manager.mu.Lock()
			manager.running = false
			if manager.stopCh == stopCh {
				manager.stopCh = nil
			}
			manager.mu.Unlock()
			return
		}
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			ticker.Reset(time.Duration(manager.currentIntervalSeconds()) * time.Second)
		}
	}
}

func (manager *quotaGuardManager) currentIntervalSeconds() int {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	interval := 0
	for _, policy := range manager.policies {
		if !policy.Enabled || policy.IntervalSeconds <= 0 {
			continue
		}
		if interval == 0 || policy.IntervalSeconds < interval {
			interval = policy.IntervalSeconds
		}
	}
	if interval <= 0 {
		return quotaGuardDefaultIntervalSecs
	}
	return interval
}

func (manager *quotaGuardManager) runScan(ctx context.Context) quotaGuardScanResponse {
	manager.mu.Lock()
	policies := cloneQuotaGuardConfigs(manager.policies)
	apiKey := strings.TrimSpace(manager.adminAPIKey)
	baseURL := strings.TrimSpace(manager.baseURL)
	manager.mu.Unlock()

	result := quotaGuardScanResponse{DryRun: quotaGuardPoliciesDryRun(policies)}
	if !quotaGuardPoliciesEnabled(policies) {
		return result
	}
	if apiKey == "" {
		result.Errors = append(result.Errors, "admin api key is missing")
		manager.recordScanResult(result, "admin api key is missing", map[int64]struct{}{}, nil, nil, nil, nil)
		return result
	}

	accounts, err := manager.fetchAccounts(ctx, baseURL, apiKey)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		if isQuotaGuardAuthError(err) {
			result.StoppedOnAuthErr = true
		}
		manager.recordScanResult(result, err.Error(), map[int64]struct{}{}, nil, nil, nil, nil)
		return result
	}
	result.ScannedAccounts = len(accounts)

	var blockIDs []int64
	var releaseIDs []int64
	var scanErrors []string
	currentManaged := make(map[int64]struct{})
	dailySpendByAccount := make(map[int64]float64)
	dailyTokensByAccount := make(map[int64]int64)
	weeklySpendByAccount := make(map[int64]float64)
	weeklyTokensByAccount := make(map[int64]int64)
	now := time.Now().UTC()
	for _, account := range accounts {
		if isQuotaGuardManaged(account) {
			currentManaged[account.ID] = struct{}{}
		}
	}

	for _, account := range accounts {
		usageByTimezone := make(map[string]quotaGuardUsage)
		if quotaGuardPoliciesHaveAnyLimit(policies) && (quotaGuardAccountInAnyScope(policies, account.ID) || isQuotaGuardManaged(account)) {
			for _, policy := range policies {
				if !policy.Enabled || !quotaGuardHasAnyLimit(policy) {
					continue
				}
				if !quotaGuardAccountInScope(policy, account.ID) && !isQuotaGuardManaged(account) {
					continue
				}
				timezoneName := policy.DailySpendTimezone
				if _, loaded := usageByTimezone[timezoneName]; loaded {
					continue
				}
				usageByTimezone[timezoneName] = quotaGuardUsage{
					Daily:  DailyUpstreamUsage(account.ID, timezoneName, now),
					Weekly: WeeklyUpstreamUsage(account.ID, timezoneName, now),
				}
			}
			displayUsage := usageByTimezone[quotaGuardTimezoneForAccount(policies, account.ID)]
			dailySpendByAccount[account.ID] = displayUsage.Daily.CostUSD
			dailyTokensByAccount[account.ID] = displayUsage.Daily.Tokens
			weeklySpendByAccount[account.ID] = displayUsage.Weekly.CostUSD
			weeklyTokensByAccount[account.ID] = displayUsage.Weekly.Tokens
		}
		decision, ok := evaluateQuotaGuardPoliciesDecision(account, policies, now, usageByTimezone)
		if !ok {
			continue
		}
		result.CandidateCount++
		switch decision.Action {
		case "block":
			if !decision.DryRun {
				if err := manager.bulkUpdateAccount(ctx, baseURL, apiKey, account.ID, false, decision.Extra); err != nil {
					scanErrors = append(scanErrors, fmt.Sprintf("block account %d failed: %v", account.ID, err))
					if isQuotaGuardAuthError(err) {
						result.StoppedOnAuthErr = true
						break
					}
					continue
				}
			}
			blockIDs = append(blockIDs, account.ID)
			currentManaged[account.ID] = struct{}{}
		case "release":
			if !decision.DryRun {
				if err := manager.bulkUpdateAccount(ctx, baseURL, apiKey, account.ID, true, decision.Extra); err != nil {
					scanErrors = append(scanErrors, fmt.Sprintf("release account %d failed: %v", account.ID, err))
					if isQuotaGuardAuthError(err) {
						result.StoppedOnAuthErr = true
						break
					}
					continue
				}
			}
			releaseIDs = append(releaseIDs, account.ID)
			delete(currentManaged, account.ID)
		}
		if result.StoppedOnAuthErr {
			break
		}
	}

	result.BlockedCount = len(blockIDs)
	result.BlockedIDs = append([]int64(nil), blockIDs...)
	result.ReleasedCount = len(releaseIDs)
	result.ReleasedIDs = append([]int64(nil), releaseIDs...)
	result.Errors = scanErrors
	result.DailySpendByAccount = cloneQuotaFloatMap(dailySpendByAccount)
	result.DailyTokensByAccount = cloneQuotaIntMap(dailyTokensByAccount)
	result.WeeklySpendByAccount = cloneQuotaFloatMap(weeklySpendByAccount)
	result.WeeklyTokensByAccount = cloneQuotaIntMap(weeklyTokensByAccount)
	manager.recordScanResult(result, strings.Join(scanErrors, "; "), currentManaged, dailySpendByAccount, dailyTokensByAccount, weeklySpendByAccount, weeklyTokensByAccount)
	return result
}

func (manager *quotaGuardManager) releaseManaged(ctx context.Context) quotaGuardScanResponse {
	manager.mu.Lock()
	apiKey := strings.TrimSpace(manager.adminAPIKey)
	baseURL := strings.TrimSpace(manager.baseURL)
	manager.mu.Unlock()

	result := quotaGuardScanResponse{}
	if apiKey == "" {
		result.Errors = append(result.Errors, "admin api key is missing")
		return result
	}
	accounts, err := manager.fetchAccounts(ctx, baseURL, apiKey)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		if isQuotaGuardAuthError(err) {
			result.StoppedOnAuthErr = true
		}
		return result
	}
	now := time.Now().UTC()
	currentManaged := make(map[int64]struct{})
	for _, account := range accounts {
		if !isQuotaGuardManaged(account) {
			continue
		}
		currentManaged[account.ID] = struct{}{}
		extra := map[string]any{
			"codex_quota_guard_managed":     false,
			"quota_guard_managed":           false,
			"codex_quota_guard_released_at": now.Format(time.RFC3339),
		}
		if err := manager.bulkUpdateAccount(ctx, baseURL, apiKey, account.ID, true, extra); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("release account %d failed: %v", account.ID, err))
			if isQuotaGuardAuthError(err) {
				result.StoppedOnAuthErr = true
				return result
			}
			continue
		}
		result.ReleasedCount++
		result.ReleasedIDs = append(result.ReleasedIDs, account.ID)
		delete(currentManaged, account.ID)
	}
	manager.recordScanResult(result, strings.Join(result.Errors, "; "), currentManaged, nil, nil, nil, nil)
	return result
}

func (manager *quotaGuardManager) recordScanResult(result quotaGuardScanResponse, lastErr string, currentManaged map[int64]struct{}, dailySpendByAccount map[int64]float64, dailyTokensByAccount map[int64]int64, weeklySpendByAccount map[int64]float64, weeklyTokensByAccount map[int64]int64) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.lastRunAt = time.Now().UTC()
	manager.lastBlockedCount = result.BlockedCount
	manager.lastBlockedIDs = append([]int64(nil), result.BlockedIDs...)
	manager.lastReleasedCount = result.ReleasedCount
	manager.lastReleasedIDs = append([]int64(nil), result.ReleasedIDs...)
	manager.lastScanCandidates = result.CandidateCount
	manager.lastScannedAccounts = result.ScannedAccounts
	manager.lastError = strings.TrimSpace(lastErr)
	manager.currentManagedIDs = make([]int64, 0, len(currentManaged))
	for id := range currentManaged {
		manager.currentManagedIDs = append(manager.currentManagedIDs, id)
	}
	slices.Sort(manager.currentManagedIDs)
	manager.currentManagedCount = len(manager.currentManagedIDs)
	manager.dailySpendByAccount = cloneQuotaFloatMap(dailySpendByAccount)
	manager.dailyTokensByAccount = cloneQuotaIntMap(dailyTokensByAccount)
	manager.weeklySpendByAccount = cloneQuotaFloatMap(weeklySpendByAccount)
	manager.weeklyTokensByAccount = cloneQuotaIntMap(weeklyTokensByAccount)
}

type quotaGuardDecision struct {
	Action string
	Extra  map[string]any
	DryRun bool
}

type quotaGuardUsage struct {
	Daily  UpstreamUsageTotals
	Weekly UpstreamUsageTotals
}

func (manager *quotaGuardManager) fetchAccounts(ctx context.Context, baseURL, apiKey string) ([]quotaGuardAccount, error) {
	page := 1
	accounts := make([]quotaGuardAccount, 0, quotaGuardPageSize)
	for {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		values.Set("page_size", strconv.Itoa(quotaGuardPageSize))
		target := strings.TrimRight(baseURL, "/") + "/api/v1/admin/accounts?" + values.Encode()
		result, statusCode, err := doQuotaGuardJSONRequest[quotaGuardAccountsEnvelope](ctx, http.MethodGet, target, apiKey, nil)
		if err != nil {
			return nil, err
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			return nil, fmt.Errorf("fetch accounts unauthorized: HTTP %d", statusCode)
		}
		if statusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("fetch accounts failed: HTTP %d", statusCode)
		}
		accounts = append(accounts, result.Data.Items...)
		if page >= result.Data.Pages || len(result.Data.Items) == 0 {
			break
		}
		page++
	}
	return accounts, nil
}

func (manager *quotaGuardManager) bulkUpdateAccount(ctx context.Context, baseURL, apiKey string, accountID int64, schedulable bool, extra map[string]any) error {
	payload := quotaGuardBulkUpdatePayload{
		AccountIDs:  []int64{accountID},
		Schedulable: &schedulable,
		Extra:       extra,
	}
	target := strings.TrimRight(baseURL, "/") + "/api/v1/admin/accounts/bulk-update"
	_, statusCode, err := doQuotaGuardJSONRequest[map[string]any](ctx, http.MethodPost, target, apiKey, payload)
	if err != nil {
		return err
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return fmt.Errorf("bulk update unauthorized: HTTP %d", statusCode)
	}
	if statusCode >= http.StatusBadRequest {
		return fmt.Errorf("bulk update failed: HTTP %d", statusCode)
	}
	return nil
}

func evaluateQuotaGuardDecision(account quotaGuardAccount, cfg quotaGuardConfig, now time.Time, daily, weekly UpstreamUsageTotals) (quotaGuardDecision, bool) {
	return evaluateQuotaGuardPoliciesDecision(account, []quotaGuardConfig{cfg}, now, map[string]quotaGuardUsage{
		cfg.DailySpendTimezone: {Daily: daily, Weekly: weekly},
	})
}

func evaluateQuotaGuardPoliciesDecision(account quotaGuardAccount, policies []quotaGuardConfig, now time.Time, usageByTimezone map[string]quotaGuardUsage) (quotaGuardDecision, bool) {
	managed := isQuotaGuardManaged(account)
	if !quotaGuardAccountInAnyScope(policies, account.ID) {
		if managed {
			return quotaGuardReleaseDecisionWithDryRun(now, quotaGuardPoliciesAllDryRun(policies)), true
		}
		return quotaGuardDecision{}, false
	}
	if !account.Schedulable && !managed {
		return quotaGuardDecision{}, false
	}

	var reasons []string
	var policyIDs []string
	var blockedUntil time.Time
	livePolicyHit := false
	for _, policy := range policies {
		if !policy.Enabled || !quotaGuardAccountInScope(policy, account.ID) {
			continue
		}
		usage := usageByTimezone[policy.DailySpendTimezone]
		policyReasons := make([]string, 0, 4)
		policyBlockedUntil := time.Time{}
		if policy.DailySpendLimitUSD > 0 && usage.Daily.CostUSD >= policy.DailySpendLimitUSD {
			policyReasons = append(policyReasons, "daily_spend_limit")
			policyBlockedUntil = nextQuotaGuardDay(now, policy.DailySpendTimezone)
		}
		if policy.DailyTokenLimit > 0 && usage.Daily.Tokens >= policy.DailyTokenLimit {
			policyReasons = append(policyReasons, "daily_token_limit")
			policyBlockedUntil = maxQuotaGuardTime(policyBlockedUntil, nextQuotaGuardDay(now, policy.DailySpendTimezone))
		}
		if policy.WeeklySpendLimitUSD > 0 && usage.Weekly.CostUSD >= policy.WeeklySpendLimitUSD {
			policyReasons = append(policyReasons, "weekly_spend_limit")
			policyBlockedUntil = maxQuotaGuardTime(policyBlockedUntil, nextQuotaGuardWeek(now, policy.DailySpendTimezone))
		}
		if policy.WeeklyTokenLimit > 0 && usage.Weekly.Tokens >= policy.WeeklyTokenLimit {
			policyReasons = append(policyReasons, "weekly_token_limit")
			policyBlockedUntil = maxQuotaGuardTime(policyBlockedUntil, nextQuotaGuardWeek(now, policy.DailySpendTimezone))
		}
		if len(policyReasons) == 0 {
			continue
		}
		policyIDs = append(policyIDs, policy.ID)
		for _, reason := range policyReasons {
			reasons = append(reasons, policy.ID+":"+reason)
		}
		blockedUntil = maxQuotaGuardTime(blockedUntil, policyBlockedUntil)
		if !policy.DryRun {
			livePolicyHit = true
		}
	}

	if len(reasons) == 0 {
		if managed {
			return quotaGuardReleaseDecisionWithDryRun(now, quotaGuardPoliciesAllDryRun(policies)), true
		}
		return quotaGuardDecision{}, false
	}

	return quotaGuardDecision{
		Action: "block",
		DryRun: !livePolicyHit,
		Extra: map[string]any{
			"codex_quota_guard_managed":        true,
			"quota_guard_managed":              true,
			"codex_quota_guard_policy_ids":     policyIDs,
			"quota_guard_policy_ids":           policyIDs,
			"codex_quota_guard_blocked_reason": strings.Join(reasons, ","),
			"codex_quota_guard_blocked_until":  blockedUntil.UTC().Format(time.RFC3339),
			"quota_guard_blocked_until":        blockedUntil.UTC().Format(time.RFC3339),
			"codex_quota_guard_blocked_at":     now.UTC().Format(time.RFC3339),
			"codex_quota_guard_policy_count":   len(policyIDs),
		},
	}, true
}

func dailyTokenLimitOrZero(limit int64) int64 {
	if limit < 0 {
		return 0
	}
	return limit
}

func quotaGuardReleaseDecision(now time.Time) quotaGuardDecision {
	return quotaGuardReleaseDecisionWithDryRun(now, false)
}

func quotaGuardReleaseDecisionWithDryRun(now time.Time, dryRun bool) quotaGuardDecision {
	return quotaGuardDecision{
		Action: "release",
		DryRun: dryRun,
		Extra: map[string]any{
			"codex_quota_guard_managed":     false,
			"quota_guard_managed":           false,
			"codex_quota_guard_released_at": now.UTC().Format(time.RFC3339),
		},
	}
}

func quotaGuardHasAnyLimit(cfg quotaGuardConfig) bool {
	return cfg.DailySpendLimitUSD > 0 || cfg.DailyTokenLimit > 0 || cfg.WeeklySpendLimitUSD > 0 || cfg.WeeklyTokenLimit > 0
}

func quotaGuardAccountInScope(cfg quotaGuardConfig, accountID int64) bool {
	if len(cfg.AccountIDs) == 0 {
		return true
	}
	for _, selectedID := range cfg.AccountIDs {
		if selectedID == accountID {
			return true
		}
	}
	return false
}

func quotaGuardAccountInAnyScope(policies []quotaGuardConfig, accountID int64) bool {
	for _, policy := range policies {
		if policy.Enabled && quotaGuardAccountInScope(policy, accountID) {
			return true
		}
	}
	return false
}

func quotaGuardTimezoneForAccount(policies []quotaGuardConfig, accountID int64) string {
	for _, policy := range policies {
		if policy.Enabled && quotaGuardAccountInScope(policy, accountID) {
			return policy.DailySpendTimezone
		}
	}
	for _, policy := range policies {
		if policy.Enabled {
			return policy.DailySpendTimezone
		}
	}
	return quotaGuardDefaultSpendTimezone
}

func quotaGuardPoliciesAllDryRun(policies []quotaGuardConfig) bool {
	seen := false
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		seen = true
		if !policy.DryRun {
			return false
		}
	}
	return seen
}

func quotaGuardPoliciesDryRun(policies []quotaGuardConfig) bool {
	return quotaGuardPoliciesAllDryRun(policies)
}

func nextQuotaGuardDay(now time.Time, timezoneName string) time.Time {
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		location = time.UTC
	}
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, location)
	return next.UTC()
}

func nextQuotaGuardWeek(now time.Time, timezoneName string) time.Time {
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		location = time.UTC
	}
	localNow := now.In(location)
	daysUntilMonday := (int(time.Monday) - int(localNow.Weekday()) + 7) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+daysUntilMonday, 0, 0, 0, 0, location)
	return next.UTC()
}

func maxQuotaGuardTime(left, right time.Time) time.Time {
	if left.IsZero() || right.After(left) {
		return right
	}
	return left
}

func isQuotaGuardManaged(account quotaGuardAccount) bool {
	for _, key := range []string{"quota_guard_managed", "codex_quota_guard_managed"} {
		value, ok := account.Extra[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			if typed {
				return true
			}
		case string:
			if strings.EqualFold(strings.TrimSpace(typed), "true") {
				return true
			}
		}
	}
	return false
}

func parseQuotaGuardFloat(value any) float64 {
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
		v, _ := typed.Float64()
		return v
	case string:
		v, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return v
	default:
		return 0
	}
}

func parseQuotaGuardTime(value any) time.Time {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func doQuotaGuardJSONRequest[T any](ctx context.Context, method, rawURL, apiKey string, payload any) (T, int, error) {
	var zero T
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return zero, 0, err
		}
		body = bytes.NewReader(raw)
	}
	target := rawURL
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		target = "http://127.0.0.1" + rawURL
	}
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return zero, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		request.Header.Set(quotaGuardHeaderAPIKey, strings.TrimSpace(apiKey))
	}
	client := &http.Client{Timeout: 30 * time.Second}
	responseBody, err := client.Do(request)
	if err != nil {
		return zero, 0, err
	}
	defer responseBody.Body.Close()
	decodeErr := json.NewDecoder(responseBody.Body).Decode(&zero)
	if decodeErr != nil && decodeErr != io.EOF {
		// Preserve the status code for callers even when an upstream proxy
		// returns an HTML or empty error body. The caller can then classify 401/403
		// and stop the background scanner instead of reporting a JSON parse error.
		if responseBody.StatusCode < http.StatusBadRequest {
			return zero, responseBody.StatusCode, decodeErr
		}
	}
	return zero, responseBody.StatusCode, nil
}

func buildQuotaGuardBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request != nil && c.Request.TLS != nil {
		scheme = "https"
	}
	host := "127.0.0.1"
	if c.Request != nil && c.Request.Host != "" {
		host = c.Request.Host
	}
	return scheme + "://" + host
}

func buildQuotaGuardLocalURL(c *gin.Context, path string) string {
	return buildQuotaGuardBaseURL(c) + path
}

func copyQuotaGuardAuthHeaders(c *gin.Context, request *http.Request) {
	if request == nil || c == nil {
		return
	}
	if auth := strings.TrimSpace(c.GetHeader("Authorization")); auth != "" {
		request.Header.Set("Authorization", auth)
	}
	if apiKey := strings.TrimSpace(c.GetHeader(quotaGuardHeaderAPIKey)); apiKey != "" {
		request.Header.Set(quotaGuardHeaderAPIKey, apiKey)
	}
}

func isQuotaGuardAuthError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "http 401") || strings.Contains(text, "http 403") || strings.Contains(text, "unauthorized")
}
