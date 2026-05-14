package routes

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
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

const (
	dataType                           = "sub2api-data"
	dataVersion                        = 1
	codexQuotaGuardPageSize            = 500
	codexQuotaGuardDefaultReserve      = 1
	codexQuotaGuardDefaultIntervalSecs = 60
	codexQuotaGuardHeaderAPIKey        = "x-api-key"
	codexQuotaGuardSourceProvided      = "provided"
	codexQuotaGuardSourceAutoCreated   = "auto_created"
	codexQuotaGuardSourceMissing       = "missing"
)

var globalCodexQuotaGuard = newCodexQuotaGuardManager()

func buildProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func defaultProxyName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "imported-proxy"
	}
	return name
}

func validateDataProxy(item admin.DataProxy) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return errors.New("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.New("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	if item.Status != "" {
		normalizedStatus := strings.TrimSpace(strings.ToLower(item.Status))
		if normalizedStatus != service.StatusActive && normalizedStatus != "inactive" && normalizedStatus != service.StatusDisabled {
			return fmt.Errorf("proxy status is invalid: %s", item.Status)
		}
	}
	return nil
}

const clashSubscriptionMaxBytes = 2 << 20

type clashPreviewRequest struct {
	URL     string `json:"url"`
	Content string `json:"content"`
}

type clashProxyDocument struct {
	Proxies []clashProxyEntry `yaml:"proxies"`
}

type clashProxyEntry struct {
	Name     any `yaml:"name"`
	Type     any `yaml:"type"`
	Server   any `yaml:"server"`
	Port     any `yaml:"port"`
	Username any `yaml:"username"`
	Password any `yaml:"password"`
	TLS      any `yaml:"tls"`
	Enabled  any `yaml:"enabled"`
}

type ClashProxyPreviewRow struct {
	Index     int             `json:"index"`
	Name      string          `json:"name"`
	Duplicate bool            `json:"duplicate"`
	Valid     bool            `json:"valid"`
	Errors    []string        `json:"errors"`
	Proxy     admin.DataProxy `json:"proxy"`
}

type ClashProxyPreviewSummary struct {
	Total      int `json:"total"`
	Valid      int `json:"valid"`
	Invalid    int `json:"invalid"`
	Duplicates int `json:"duplicates"`
}

type ClashBatchPayload struct {
	Proxies []ClashBatchProxy `json:"proxies"`
}

type ClashBatchProxy struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type ClashProxyPreviewResponse struct {
	Source       string                   `json:"source"`
	Rows         []ClashProxyPreviewRow   `json:"rows"`
	Summary      ClashProxyPreviewSummary `json:"summary"`
	DataPayload  admin.DataPayload        `json:"data_payload"`
	BatchPayload ClashBatchPayload        `json:"batch_payload"`
}

type codexQuotaGuardStartRequest struct {
	Enabled         *bool    `json:"enabled"`
	ReservePercent  *float64 `json:"reserve_percent"`
	Windows         []string `json:"windows"`
	IntervalSeconds *int     `json:"interval_seconds"`
	AccountTypes    []string `json:"account_types"`
	DryRun          bool     `json:"dry_run"`
}

type codexQuotaGuardConfig struct {
	Enabled         bool     `json:"enabled"`
	ReservePercent  float64  `json:"reserve_percent"`
	Windows         []string `json:"windows"`
	IntervalSeconds int      `json:"interval_seconds"`
	AccountTypes    []string `json:"account_types"`
	DryRun          bool     `json:"dry_run"`
}

type codexQuotaGuardStatusResponse struct {
	Running             bool                  `json:"running"`
	Config              codexQuotaGuardConfig `json:"config"`
	AdminAPIKeySource   string                `json:"admin_api_key_source"`
	AdminAPIKeyCached   bool                  `json:"admin_api_key_cached"`
	LastRunAt           string                `json:"last_run_at,omitempty"`
	LastError           string                `json:"last_error,omitempty"`
	LastBlockedCount    int                   `json:"last_blocked_count"`
	LastBlockedIDs      []int64               `json:"last_blocked_ids,omitempty"`
	LastReleasedCount   int                   `json:"last_released_count"`
	LastReleasedIDs     []int64               `json:"last_released_ids,omitempty"`
	LastScanCandidates  int                   `json:"last_scan_candidates"`
	LastScannedAccounts int                   `json:"last_scanned_accounts"`
}

type codexQuotaGuardScanResponse struct {
	BlockedCount     int      `json:"blocked_count"`
	BlockedIDs       []int64  `json:"blocked_ids,omitempty"`
	ReleasedCount    int      `json:"released_count"`
	ReleasedIDs      []int64  `json:"released_ids,omitempty"`
	CandidateCount   int      `json:"candidate_count"`
	ScannedAccounts  int      `json:"scanned_accounts"`
	DryRun           bool     `json:"dry_run"`
	Errors           []string `json:"errors,omitempty"`
	StoppedOnAuthErr bool     `json:"stopped_on_auth_error"`
}

type codexQuotaGuardManager struct {
	mu sync.Mutex

	running             bool
	config              codexQuotaGuardConfig
	adminAPIKey         string
	adminAPIKeySource   string
	baseURL             string
	lastRunAt           time.Time
	lastError           string
	lastBlockedCount    int
	lastBlockedIDs      []int64
	lastReleasedCount   int
	lastReleasedIDs     []int64
	lastScanCandidates  int
	lastScannedAccounts int
	stopCh              chan struct{}
}

type codexGuardAccountsEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Items []codexGuardAccount `json:"items"`
		Total int64               `json:"total"`
		Page  int                 `json:"page"`
		Pages int                 `json:"pages"`
	} `json:"data"`
	Message string `json:"message"`
}

type codexGuardAccount struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	Schedulable bool           `json:"schedulable"`
	Extra       map[string]any `json:"extra"`
}

type codexGuardBulkUpdatePayload struct {
	AccountIDs  []int64        `json:"account_ids"`
	Schedulable *bool          `json:"schedulable,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

type codexGuardAdminAPIKeyStatusEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Exists bool `json:"exists"`
	} `json:"data"`
	Message string `json:"message"`
}

type codexGuardAdminAPIKeyRegenerateEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Key string `json:"key"`
	} `json:"data"`
	Message string `json:"message"`
}

func registerPluginAdminTools(adminGroup *gin.RouterGroup) {
	adminGroup.POST("/proxies/import/clash", previewClashImport)
	adminGroup.POST("/codex-quota-guard/start", codexQuotaGuardStart)
	adminGroup.POST("/codex-quota-guard/stop", codexQuotaGuardStop)
	adminGroup.GET("/codex-quota-guard/status", codexQuotaGuardStatus)
	adminGroup.POST("/codex-quota-guard/scan", codexQuotaGuardScan)
	adminGroup.POST("/codex-quota-guard/release", codexQuotaGuardRelease)
}

func newCodexQuotaGuardManager() *codexQuotaGuardManager {
	return &codexQuotaGuardManager{
		config: codexQuotaGuardConfig{
			Enabled:         true,
			ReservePercent:  codexQuotaGuardDefaultReserve,
			Windows:         []string{"5h", "7d"},
			IntervalSeconds: codexQuotaGuardDefaultIntervalSecs,
			AccountTypes:    []string{"oauth"},
			DryRun:          false,
		},
		adminAPIKeySource: codexQuotaGuardSourceMissing,
	}
}

func codexQuotaGuardStart(c *gin.Context) {
	var req codexQuotaGuardStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	cfg, err := normalizeCodexQuotaGuardConfig(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	apiKey, source, err := globalCodexQuotaGuard.resolveAdminAPIKey(c, cfg)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	status := globalCodexQuotaGuard.start(apiKey, source, buildCodexQuotaGuardBaseURL(c), cfg)
	response.Success(c, status)
}

func codexQuotaGuardStop(c *gin.Context) {
	status := globalCodexQuotaGuard.stop()
	response.Success(c, status)
}

func codexQuotaGuardStatus(c *gin.Context) {
	response.Success(c, globalCodexQuotaGuard.status())
}

func codexQuotaGuardScan(c *gin.Context) {
	result := globalCodexQuotaGuard.runScan(c.Request.Context())
	if result.StoppedOnAuthErr {
		globalCodexQuotaGuard.stop()
	}
	response.Success(c, result)
}

func codexQuotaGuardRelease(c *gin.Context) {
	result := globalCodexQuotaGuard.releaseManaged(c.Request.Context())
	if result.StoppedOnAuthErr {
		globalCodexQuotaGuard.stop()
	}
	response.Success(c, result)
}

// previewClashImport parses pasted Clash YAML or fetches a Clash subscription URL.
// It only returns import-ready payloads; writes still go through existing proxy APIs.
func previewClashImport(c *gin.Context) {
	var req clashPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	content := strings.TrimSpace(req.Content)
	source := "content"
	if content == "" {
		rawURL := strings.TrimSpace(req.URL)
		if rawURL == "" {
			response.BadRequest(c, "subscription url or yaml content is required")
			return
		}

		fetched, err := fetchClashSubscription(c.Request.Context(), rawURL)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		content = fetched
		source = "url"
	} else if len(content) > clashSubscriptionMaxBytes {
		response.BadRequest(c, "subscription content is too large")
		return
	}

	preview, err := buildClashProxyPreview(content, source)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, preview)
}

func fetchClashSubscription(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("subscription url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("subscription url must use http or https")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("subscription url is invalid")
	}
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", "clash.meta/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch subscription failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("fetch subscription failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, clashSubscriptionMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read subscription failed: %w", err)
	}
	if len(body) > clashSubscriptionMaxBytes {
		return "", fmt.Errorf("subscription content is too large")
	}
	return string(body), nil
}

func buildClashProxyPreview(content, source string) (ClashProxyPreviewResponse, error) {
	var doc clashProxyDocument
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return ClashProxyPreviewResponse{}, fmt.Errorf("parse Clash YAML failed: %w", err)
	}
	if len(doc.Proxies) == 0 {
		return ClashProxyPreviewResponse{}, fmt.Errorf("YAML does not contain proxies")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	out := ClashProxyPreviewResponse{
		Source: source,
		Rows:   make([]ClashProxyPreviewRow, 0, len(doc.Proxies)),
		DataPayload: admin.DataPayload{
			Type:       dataType,
			Version:    dataVersion,
			ExportedAt: now,
			Proxies:    []admin.DataProxy{},
			Accounts:   []admin.DataAccount{},
		},
		BatchPayload: ClashBatchPayload{Proxies: []ClashBatchProxy{}},
	}

	seen := make(map[string]int, len(doc.Proxies))
	for i := range doc.Proxies {
		row := buildClashPreviewRow(doc.Proxies[i], i+1)
		if row.Valid {
			key := row.Proxy.ProxyKey
			if firstIndex, ok := seen[key]; ok {
				row.Duplicate = true
				row.Valid = false
				row.Errors = append(row.Errors, fmt.Sprintf("duplicate proxy, first seen at row %d", firstIndex))
				out.Summary.Duplicates++
			} else {
				seen[key] = row.Index
				out.DataPayload.Proxies = append(out.DataPayload.Proxies, row.Proxy)
				out.BatchPayload.Proxies = append(out.BatchPayload.Proxies, ClashBatchProxy{
					Protocol: row.Proxy.Protocol,
					Host:     row.Proxy.Host,
					Port:     row.Proxy.Port,
					Username: row.Proxy.Username,
					Password: row.Proxy.Password,
				})
			}
		}

		if row.Valid {
			out.Summary.Valid++
		} else {
			out.Summary.Invalid++
		}
		out.Rows = append(out.Rows, row)
	}
	out.Summary.Total = len(out.Rows)
	return out, nil
}

func buildClashPreviewRow(entry clashProxyEntry, index int) ClashProxyPreviewRow {
	name := strings.TrimSpace(clashScalarString(entry.Name))
	rawType := strings.ToLower(strings.TrimSpace(clashScalarString(entry.Type)))
	host := strings.TrimSpace(clashScalarString(entry.Server))
	port, portOK := clashScalarInt(entry.Port)
	username := strings.TrimSpace(clashScalarString(entry.Username))
	password := strings.TrimSpace(clashScalarString(entry.Password))
	tls := clashScalarBool(entry.TLS)

	status := service.StatusActive
	if enabled, ok := clashOptionalBool(entry.Enabled); ok && !enabled {
		status = "inactive"
	}

	protocol, protocolOK := mapClashProxyProtocol(rawType, tls)
	proxy := admin.DataProxy{
		Name:     defaultProxyName(name),
		Protocol: protocol,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		Status:   status,
	}
	proxy.ProxyKey = buildProxyKey(proxy.Protocol, proxy.Host, proxy.Port, proxy.Username, proxy.Password)

	row := ClashProxyPreviewRow{
		Index:  index,
		Name:   name,
		Proxy:  proxy,
		Errors: []string{},
	}
	if !protocolOK {
		if rawType == "" {
			row.Errors = append(row.Errors, "proxy type is required")
		} else {
			row.Errors = append(row.Errors, "unsupported proxy type: "+rawType)
		}
	}
	if host == "" {
		row.Errors = append(row.Errors, "server is required")
	}
	if !portOK || port <= 0 || port > 65535 {
		row.Errors = append(row.Errors, "port is invalid")
	}
	if len(row.Errors) == 0 {
		if err := validateDataProxy(proxy); err != nil {
			row.Errors = append(row.Errors, err.Error())
		}
	}
	row.Valid = len(row.Errors) == 0
	return row
}

func normalizeCodexQuotaGuardConfig(req codexQuotaGuardStartRequest) (codexQuotaGuardConfig, error) {
	cfg := codexQuotaGuardConfig{
		Enabled:         true,
		ReservePercent:  codexQuotaGuardDefaultReserve,
		Windows:         []string{"5h", "7d"},
		IntervalSeconds: codexQuotaGuardDefaultIntervalSecs,
		AccountTypes:    []string{"oauth"},
		DryRun:          req.DryRun,
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.ReservePercent != nil {
		cfg.ReservePercent = *req.ReservePercent
	}
	if req.IntervalSeconds != nil {
		cfg.IntervalSeconds = *req.IntervalSeconds
	}
	if len(req.Windows) > 0 {
		cfg.Windows = normalizeStringList(req.Windows)
	}
	if len(req.AccountTypes) > 0 {
		cfg.AccountTypes = normalizeStringList(req.AccountTypes)
	}
	if cfg.ReservePercent < 0 || cfg.ReservePercent >= 100 {
		return cfg, errors.New("reserve_percent must be >= 0 and < 100")
	}
	if cfg.IntervalSeconds <= 0 {
		return cfg, errors.New("interval_seconds must be > 0")
	}
	if len(cfg.Windows) == 0 {
		return cfg, errors.New("windows must not be empty")
	}
	validWindows := map[string]struct{}{"5h": {}, "7d": {}}
	for _, window := range cfg.Windows {
		if _, ok := validWindows[window]; !ok {
			return cfg, fmt.Errorf("unsupported window: %s", window)
		}
	}
	if len(cfg.AccountTypes) == 0 {
		return cfg, errors.New("account_types must not be empty")
	}
	return cfg, nil
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func (m *codexQuotaGuardManager) start(apiKey, source, baseURL string, cfg codexQuotaGuardConfig) codexQuotaGuardStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopCh != nil {
		close(m.stopCh)
	}
	m.stopCh = make(chan struct{})
	m.running = cfg.Enabled
	m.config = cfg
	m.adminAPIKey = strings.TrimSpace(apiKey)
	m.adminAPIKeySource = source
	m.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	m.lastError = ""

	if cfg.Enabled {
		go m.loop(m.stopCh)
	}
	return m.snapshotLocked()
}

func (m *codexQuotaGuardManager) stop() codexQuotaGuardStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stopCh != nil {
		close(m.stopCh)
		m.stopCh = nil
	}
	m.running = false
	m.adminAPIKey = ""
	m.adminAPIKeySource = codexQuotaGuardSourceMissing
	m.baseURL = ""
	return m.snapshotLocked()
}

func (m *codexQuotaGuardManager) status() codexQuotaGuardStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *codexQuotaGuardManager) snapshotLocked() codexQuotaGuardStatusResponse {
	resp := codexQuotaGuardStatusResponse{
		Running:             m.running,
		Config:              m.config,
		AdminAPIKeySource:   codexQuotaGuardSourceMissing,
		AdminAPIKeyCached:   strings.TrimSpace(m.adminAPIKey) != "",
		LastError:           m.lastError,
		LastBlockedCount:    m.lastBlockedCount,
		LastBlockedIDs:      append([]int64(nil), m.lastBlockedIDs...),
		LastReleasedCount:   m.lastReleasedCount,
		LastReleasedIDs:     append([]int64(nil), m.lastReleasedIDs...),
		LastScanCandidates:  m.lastScanCandidates,
		LastScannedAccounts: m.lastScannedAccounts,
	}
	if strings.TrimSpace(m.adminAPIKeySource) != "" {
		resp.AdminAPIKeySource = m.adminAPIKeySource
	}
	if !m.lastRunAt.IsZero() {
		resp.LastRunAt = m.lastRunAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func (m *codexQuotaGuardManager) resolveAdminAPIKey(c *gin.Context, cfg codexQuotaGuardConfig) (string, string, error) {
	if key := strings.TrimSpace(c.GetHeader(codexQuotaGuardHeaderAPIKey)); key != "" {
		return key, codexQuotaGuardSourceProvided, nil
	}

	if !cfg.Enabled {
		return "", codexQuotaGuardSourceMissing, nil
	}

	statusURL := buildCodexQuotaGuardLocalURL(c, "/api/v1/admin/settings/admin-api-key")
	keyResp, statusCode, err := doCodexQuotaGuardJSONRequest[codexGuardAdminAPIKeyStatusEnvelope](c.Request.Context(), http.MethodGet, statusURL, "", nil)
	if err != nil {
		return "", codexQuotaGuardSourceMissing, err
	}
	if statusCode >= http.StatusBadRequest {
		return "", codexQuotaGuardSourceMissing, fmt.Errorf("query admin api key status failed: HTTP %d", statusCode)
	}
	if keyResp.Data.Exists {
		return "", codexQuotaGuardSourceMissing, errors.New("admin api key already exists; please start with x-api-key header")
	}

	createURL := buildCodexQuotaGuardLocalURL(c, "/api/v1/admin/settings/admin-api-key/regenerate")
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, createURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", codexQuotaGuardSourceMissing, err
	}
	req.Header.Set("Content-Type", "application/json")
	copyCodexQuotaGuardAuthHeaders(c, req)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", codexQuotaGuardSourceMissing, fmt.Errorf("create admin api key failed: %w", err)
	}
	defer resp.Body.Close()
	var createResp codexGuardAdminAPIKeyRegenerateEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", codexQuotaGuardSourceMissing, fmt.Errorf("decode created admin api key failed: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", codexQuotaGuardSourceMissing, fmt.Errorf("create admin api key failed: HTTP %d", resp.StatusCode)
	}
	if strings.TrimSpace(createResp.Data.Key) == "" {
		return "", codexQuotaGuardSourceMissing, errors.New("created admin api key is empty")
	}
	return strings.TrimSpace(createResp.Data.Key), codexQuotaGuardSourceAutoCreated, nil
}

func (m *codexQuotaGuardManager) loop(stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(m.currentIntervalSeconds()) * time.Second)
	defer ticker.Stop()

	for {
		result := m.runScan(context.Background())
		if result.StoppedOnAuthErr {
			m.mu.Lock()
			m.running = false
			if m.stopCh == stopCh {
				m.stopCh = nil
			}
			m.mu.Unlock()
			return
		}

		select {
		case <-stopCh:
			return
		case <-ticker.C:
			ticker.Reset(time.Duration(m.currentIntervalSeconds()) * time.Second)
		}
	}
}

func (m *codexQuotaGuardManager) currentIntervalSeconds() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.IntervalSeconds <= 0 {
		return codexQuotaGuardDefaultIntervalSecs
	}
	return m.config.IntervalSeconds
}

func (m *codexQuotaGuardManager) runScan(ctx context.Context) codexQuotaGuardScanResponse {
	m.mu.Lock()
	cfg := m.config
	apiKey := strings.TrimSpace(m.adminAPIKey)
	baseURL := strings.TrimSpace(m.baseURL)
	m.mu.Unlock()

	result := codexQuotaGuardScanResponse{DryRun: cfg.DryRun}
	if !cfg.Enabled {
		return result
	}
	if apiKey == "" {
		result.Errors = append(result.Errors, "admin api key is missing")
		m.recordScanResult(result, "admin api key is missing")
		return result
	}

	accounts, err := m.fetchAccounts(ctx, baseURL, apiKey)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		if isCodexQuotaGuardAuthError(err) {
			result.StoppedOnAuthErr = true
		}
		m.recordScanResult(result, err.Error())
		return result
	}
	result.ScannedAccounts = len(accounts)

	var blockIDs []int64
	var releaseIDs []int64
	var scanErrors []string

	for _, account := range accounts {
		decision, ok := evaluateCodexQuotaGuardDecision(account, cfg, time.Now().UTC())
		if !ok {
			continue
		}
		result.CandidateCount++
		switch decision.Action {
		case "block":
			if !cfg.DryRun {
				if err := m.bulkUpdateAccount(ctx, apiKey, account.ID, false, decision.Extra); err != nil {
					scanErrors = append(scanErrors, fmt.Sprintf("block account %d failed: %v", account.ID, err))
					if isCodexQuotaGuardAuthError(err) {
						result.StoppedOnAuthErr = true
						break
					}
					continue
				}
			}
			blockIDs = append(blockIDs, account.ID)
		case "release":
			if !cfg.DryRun {
				if err := m.bulkUpdateAccount(ctx, apiKey, account.ID, true, decision.Extra); err != nil {
					scanErrors = append(scanErrors, fmt.Sprintf("release account %d failed: %v", account.ID, err))
					if isCodexQuotaGuardAuthError(err) {
						result.StoppedOnAuthErr = true
						break
					}
					continue
				}
			}
			releaseIDs = append(releaseIDs, account.ID)
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
	lastErr := strings.Join(scanErrors, "; ")
	m.recordScanResult(result, lastErr)
	return result
}

func (m *codexQuotaGuardManager) releaseManaged(ctx context.Context) codexQuotaGuardScanResponse {
	m.mu.Lock()
	apiKey := strings.TrimSpace(m.adminAPIKey)
	baseURL := strings.TrimSpace(m.baseURL)
	m.mu.Unlock()

	result := codexQuotaGuardScanResponse{}
	if apiKey == "" {
		result.Errors = append(result.Errors, "admin api key is missing")
		return result
	}
	accounts, err := m.fetchAccounts(ctx, baseURL, apiKey)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		if isCodexQuotaGuardAuthError(err) {
			result.StoppedOnAuthErr = true
		}
		return result
	}
	now := time.Now().UTC()
	for _, account := range accounts {
		if !isCodexGuardManaged(account) {
			continue
		}
		extra := map[string]any{
			"codex_quota_guard_managed":     false,
			"codex_quota_guard_released_at": now.Format(time.RFC3339),
		}
		if err := m.bulkUpdateAccount(ctx, apiKey, account.ID, true, extra); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("release account %d failed: %v", account.ID, err))
			if isCodexQuotaGuardAuthError(err) {
				result.StoppedOnAuthErr = true
				return result
			}
			continue
		}
		result.ReleasedCount++
		result.ReleasedIDs = append(result.ReleasedIDs, account.ID)
	}
	return result
}

func (m *codexQuotaGuardManager) recordScanResult(result codexQuotaGuardScanResponse, lastErr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRunAt = time.Now().UTC()
	m.lastBlockedCount = result.BlockedCount
	m.lastBlockedIDs = append([]int64(nil), result.BlockedIDs...)
	m.lastReleasedCount = result.ReleasedCount
	m.lastReleasedIDs = append([]int64(nil), result.ReleasedIDs...)
	m.lastScanCandidates = result.CandidateCount
	m.lastScannedAccounts = result.ScannedAccounts
	m.lastError = strings.TrimSpace(lastErr)
}

func (m *codexQuotaGuardManager) fetchAccounts(ctx context.Context, baseURL, apiKey string) ([]codexGuardAccount, error) {
	page := 1
	out := make([]codexGuardAccount, 0, codexQuotaGuardPageSize)
	for {
		values := url.Values{}
		values.Set("platform", service.PlatformOpenAI)
		values.Set("type", service.AccountTypeOAuth)
		values.Set("status", service.StatusActive)
		values.Set("page", strconv.Itoa(page))
		values.Set("page_size", strconv.Itoa(codexQuotaGuardPageSize))
		target := strings.TrimRight(baseURL, "/") + "/api/v1/admin/accounts?" + values.Encode()
		resp, statusCode, err := doCodexQuotaGuardJSONRequest[codexGuardAccountsEnvelope](ctx, http.MethodGet, target, apiKey, nil)
		if err != nil {
			return nil, err
		}
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			return nil, fmt.Errorf("fetch accounts unauthorized: HTTP %d", statusCode)
		}
		if statusCode >= http.StatusBadRequest {
			return nil, fmt.Errorf("fetch accounts failed: HTTP %d", statusCode)
		}
		out = append(out, resp.Data.Items...)
		if page >= resp.Data.Pages || len(resp.Data.Items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (m *codexQuotaGuardManager) bulkUpdateAccount(ctx context.Context, apiKey string, accountID int64, schedulable bool, extra map[string]any) error {
	payload := codexGuardBulkUpdatePayload{
		AccountIDs:  []int64{accountID},
		Schedulable: &schedulable,
		Extra:       extra,
	}
	_, statusCode, err := doCodexQuotaGuardJSONRequest[map[string]any](ctx, http.MethodPost, "/api/v1/admin/accounts/bulk-update", apiKey, payload)
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

type codexQuotaGuardDecision struct {
	Action string
	Extra  map[string]any
}

func evaluateCodexQuotaGuardDecision(account codexGuardAccount, cfg codexQuotaGuardConfig, now time.Time) (codexQuotaGuardDecision, bool) {
	if account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeOAuth {
		return codexQuotaGuardDecision{}, false
	}
	if !account.Schedulable && !isCodexGuardManaged(account) {
		return codexQuotaGuardDecision{}, false
	}

	if isCodexGuardManaged(account) {
		until := parseCodexQuotaGuardTime(account.Extra["codex_quota_guard_blocked_until"])
		if !until.IsZero() && !now.Before(until) {
			return codexQuotaGuardDecision{
				Action: "release",
				Extra: map[string]any{
					"codex_quota_guard_managed":     false,
					"codex_quota_guard_released_at": now.Format(time.RFC3339),
				},
			}, true
		}
	}

	threshold := 100 - cfg.ReservePercent
	var hitWindows []string
	var maxPercent float64
	var blockedUntil time.Time
	for _, window := range cfg.Windows {
		usedPercent := parseCodexQuotaGuardFloat(account.Extra[fmt.Sprintf("codex_%s_used_percent", window)])
		if usedPercent < threshold {
			continue
		}
		resetAt := parseCodexQuotaGuardTime(account.Extra[fmt.Sprintf("codex_%s_reset_at", window)])
		if resetAt.IsZero() || !now.Before(resetAt) {
			continue
		}
		hitWindows = append(hitWindows, window)
		if usedPercent > maxPercent {
			maxPercent = usedPercent
		}
		if blockedUntil.IsZero() || resetAt.After(blockedUntil) {
			blockedUntil = resetAt
		}
	}
	if len(hitWindows) == 0 || !account.Schedulable {
		return codexQuotaGuardDecision{}, false
	}
	return codexQuotaGuardDecision{
		Action: "block",
		Extra: map[string]any{
			"codex_quota_guard_managed":         true,
			"codex_quota_guard_blocked_window":  strings.Join(hitWindows, ","),
			"codex_quota_guard_blocked_until":   blockedUntil.UTC().Format(time.RFC3339),
			"codex_quota_guard_blocked_percent": maxPercent,
			"codex_quota_guard_blocked_at":      now.UTC().Format(time.RFC3339),
		},
	}, true
}

func isCodexGuardManaged(account codexGuardAccount) bool {
	value, ok := account.Extra["codex_quota_guard_managed"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func parseCodexQuotaGuardFloat(value any) float64 {
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

func parseCodexQuotaGuardTime(value any) time.Time {
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

func doCodexQuotaGuardJSONRequest[T any](ctx context.Context, method, rawURL, apiKey string, payload any) (T, int, error) {
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
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return zero, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set(codexQuotaGuardHeaderAPIKey, strings.TrimSpace(apiKey))
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return zero, 0, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&zero); err != nil && resp.ContentLength != 0 {
		return zero, resp.StatusCode, err
	}
	return zero, resp.StatusCode, nil
}

func buildCodexQuotaGuardBaseURL(c *gin.Context) string {
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

func buildCodexQuotaGuardLocalURL(c *gin.Context, path string) string {
	return buildCodexQuotaGuardBaseURL(c) + path
}

func copyCodexQuotaGuardAuthHeaders(c *gin.Context, req *http.Request) {
	if req == nil || c == nil {
		return
	}
	if auth := strings.TrimSpace(c.GetHeader("Authorization")); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if apiKey := strings.TrimSpace(c.GetHeader(codexQuotaGuardHeaderAPIKey)); apiKey != "" {
		req.Header.Set(codexQuotaGuardHeaderAPIKey, apiKey)
	}
}

func isCodexQuotaGuardAuthError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "HTTP 401") || strings.Contains(text, "HTTP 403") || strings.Contains(strings.ToLower(text), "unauthorized")
}

func mapClashProxyProtocol(rawType string, tls bool) (string, bool) {
	switch rawType {
	case "http":
		if tls {
			return "https", true
		}
		return "http", true
	case "https":
		return "https", true
	case "socks5":
		if tls {
			return "socks5h", true
		}
		return "socks5", true
	case "socks5h":
		return "socks5h", true
	default:
		return "", false
	}
}

func clashScalarString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(v)
	}
}

func clashScalarInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		if v < 0 || v > 65535 {
			return 0, false
		}
		return int(v), true
	case uint64:
		if v > 65535 {
			return 0, false
		}
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	default:
		return 0, false
	}
}

func clashScalarBool(value any) bool {
	if parsed, ok := clashOptionalBool(value); ok {
		return parsed
	}
	return false
}

func clashOptionalBool(value any) (bool, bool) {
	switch v := value.(type) {
	case nil:
		return false, false
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}
