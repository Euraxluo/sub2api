package routes

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	pluginruntime "github.com/Wei-Shaw/sub2api/internal/plugin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const (
	dataType    = "sub2api-data"
	dataVersion = 1
)

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

func registerPluginAdminTools(adminGroup *gin.RouterGroup) {
	adminGroup.POST("/proxies/import/clash", previewClashImport)
	pluginruntime.RegisterAdminJobNotifier(func(ctx context.Context, notification pluginruntime.AdminJobNotification) error {
		severity := notification.Severity
		if severity == "error" {
			severity = "critical"
		}
		umDispatchNotifications(ctx, []upstreamAlert{{
			TargetID: notification.TaskID, TargetName: notification.TaskName,
			Type: "admin_job", Severity: severity,
			Message: notification.Title, Detail: notification.Body, TriggeredAt: time.Now(),
		}})
		return nil
	})
	pluginruntime.RegisterAdminRoutes(adminGroup)

	// 飞书通知通道（lark-cli 薄封装）
	feishu := adminGroup.Group("/feishu")
	feishu.POST("/connect/start", feishuConnectStart)
	feishu.GET("/connect/status", feishuConnectStatus)
	feishu.POST("/connect/cancel", feishuConnectCancel)
	feishu.GET("/status", feishuStatus)
	feishu.GET("/chats", feishuChats)
	feishu.PUT("/target", feishuSetTarget)
	feishu.POST("/send-test", feishuSendTest)
	feishu.POST("/send", feishuSend)

	// 通知提供商选择和钉钉 dws CLI 通道
	notifications := adminGroup.Group("/notifications")
	notifications.GET("/providers", notificationProviders)
	notifications.PUT("/provider", notificationSetProvider)
	dingtalk := adminGroup.Group("/dingtalk")
	dingtalk.POST("/connect/start", dingtalkConnectStart)
	dingtalk.GET("/connect/status", dingtalkConnectStatus)
	dingtalk.POST("/connect/cancel", dingtalkConnectCancel)
	dingtalk.GET("/status", dingtalkStatus)
	dingtalk.GET("/groups", dingtalkGroups)
	dingtalk.GET("/robots", dingtalkRobots)
	dingtalk.POST("/robot/create", dingtalkCreateRobot)
	dingtalk.GET("/robot/create/status", dingtalkCreateRobotStatus)
	dingtalk.POST("/target/bind", dingtalkBindTarget)
	dingtalk.PUT("/target", dingtalkSetTarget)
	dingtalk.POST("/send-test", dingtalkSendTest)

	wechat := adminGroup.Group("/wechat")
	wechat.POST("/connect/start", wechatConnectStart)
	wechat.GET("/connect/status", wechatConnectStatus)
	wechat.POST("/connect/cancel", wechatConnectCancel)
	wechat.GET("/status", wechatStatus)
	wechat.POST("/send-test", wechatSendTest)

	// 上游监控
	monitor := adminGroup.Group("/upstream-monitor")
	monitor.POST("/start", upstreamMonitorStart)
	monitor.POST("/stop", upstreamMonitorStop)
	monitor.GET("/status", upstreamMonitorStatus)
	monitor.POST("/collect", upstreamMonitorCollectNow)
	monitor.GET("/snapshots", upstreamMonitorSnapshots)
	monitor.GET("/alerts", upstreamMonitorAlerts)
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

// ===========================================================================
// 飞书通知通道（lark-cli 薄封装）
// ===========================================================================

type feishuManager struct {
	mu sync.Mutex

	// lark-cli binary path
	cliPath string

	// connect session
	connecting      bool
	connectCmd      *exec.Cmd
	verificationURL string
	connectError    string
	connectDone     bool
	phase           string
	statusMessage   string

	// target
	chatID string
	userID string

	// profile dir for lark-cli (isolated from host)
	profileDir string
	targetPath string
}

const (
	feishuPhaseIdle                  = "idle"
	feishuPhaseStarting              = "starting"
	feishuPhaseAwaitingAuthorization = "awaiting_authorization"
	feishuPhaseConnected             = "connected"
	feishuPhaseReady                 = "ready"
	feishuPhaseError                 = "error"
)

var globalFeishu = newFeishuManager()

func newFeishuManager() *feishuManager {
	m := &feishuManager{
		profileDir: "/app/data/lark-cli-profile",
		targetPath: "/app/data/lark-cli-profile/target.json",
		phase:      feishuPhaseIdle,
	}
	m.loadTarget()
	return m
}

func (m *feishuManager) loadTarget() {
	data, err := os.ReadFile(m.targetPath)
	if err != nil {
		return
	}
	var target feishuTargetRequest
	if json.Unmarshal(data, &target) != nil {
		return
	}
	m.mu.Lock()
	m.chatID = strings.TrimSpace(target.ChatID)
	m.userID = strings.TrimSpace(target.UserID)
	m.mu.Unlock()
}

func (m *feishuManager) saveTarget(target feishuTargetRequest) error {
	if err := os.MkdirAll(m.profileDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(target)
	if err != nil {
		return err
	}
	tmpPath := m.targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, m.targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (m *feishuManager) cliEnv() []string {
	env := os.Environ()
	homeSet := false
	for i, value := range env {
		if strings.HasPrefix(value, "HOME=") {
			env[i] = "HOME=" + m.profileDir
			homeSet = true
			break
		}
	}
	if !homeSet {
		env = append(env, "HOME="+m.profileDir)
	}
	return append(env, "LARK_CLI_PROFILE_DIR="+m.profileDir)
}

// resolveCLI finds the preinstalled lark-cli binary. Installing packages from an
// HTTP request is intentionally avoided because the server runs as a non-root
// user and /app/data is a persistent volume.
func (m *feishuManager) resolveCLI() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cliPath != "" {
		if _, err := os.Stat(m.cliPath); err == nil {
			return m.cliPath, nil
		}
		m.cliPath = ""
	}

	// Try PATH
	if p, err := exec.LookPath("lark-cli"); err == nil {
		m.cliPath = p
		return p, nil
	}

	// Keep legacy locations readable for installations that already have a CLI
	// in the data volume, but do not create or mutate them here.
	candidates := []string{
		"/usr/local/bin/lark-cli",
		"/app/data/bin/lark-cli",
		"/app/data/bin/bin/lark-cli",
		"./data/bin/lark-cli",
		"./data/bin/bin/lark-cli",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			m.cliPath = c
			return c, nil
		}
	}

	return "", errors.New("lark-cli is not installed; rebuild the Sub2API image with @larksuite/cli@1.0.80")
}

func (m *feishuManager) runCLI(ctx context.Context, args ...string) (string, error) {
	cli, err := m.resolveCLI()
	if err != nil {
		return "", err
	}
	_ = os.MkdirAll(m.profileDir, 0o755)
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Env = m.cliEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// --- Route Handlers ---

func feishuConnectStart(c *gin.Context) {
	m := globalFeishu
	m.mu.Lock()
	if m.connecting {
		m.mu.Unlock()
		response.BadRequest(c, "connect session already in progress")
		return
	}
	m.connecting = true
	m.connectDone = false
	m.connectError = ""
	m.verificationURL = ""
	m.phase = feishuPhaseStarting
	m.statusMessage = "正在启动飞书应用授权..."
	m.mu.Unlock()

	cli, err := m.resolveCLI()
	if err != nil {
		m.mu.Lock()
		m.connecting = false
		m.phase = feishuPhaseError
		m.statusMessage = "飞书 CLI 未能启动"
		m.mu.Unlock()
		response.InternalError(c, err.Error())
		return
	}

	_ = os.MkdirAll(m.profileDir, 0o755)
	cmd := exec.Command(cli, "config", "init", "--new")
	cmd.Env = m.cliEnv()

	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout // merge
	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.connecting = false
		m.connectError = err.Error()
		m.mu.Unlock()
		response.InternalError(c, "failed to start: "+err.Error())
		return
	}

	m.mu.Lock()
	m.connectCmd = cmd
	m.mu.Unlock()

	// Background: read output for verification URL, wait for completion
	go func() {
		buf := make([]byte, 0, 8192)
		tmp := make([]byte, 1024)
		for {
			n, err := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				text := string(buf)
				// Look for URL in output
				for _, line := range strings.Split(text, "\n") {
					line = strings.TrimSpace(line)
					if strings.Contains(line, "http") && (strings.Contains(line, "verification") || strings.Contains(line, "accounts.feishu") || strings.Contains(line, "open.feishu")) {
						// Extract URL
						start := strings.Index(line, "http")
						if start >= 0 {
							urlStr := line[start:]
							// Trim trailing non-URL chars
							urlStr = strings.TrimRight(urlStr, " \t\r\n\"')>")
							m.mu.Lock()
							m.verificationURL = urlStr
							m.phase = feishuPhaseAwaitingAuthorization
							m.statusMessage = "请扫描二维码完成应用授权"
							m.mu.Unlock()
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
		waitErr := cmd.Wait()
		m.mu.Lock()
		m.connecting = false
		m.connectDone = waitErr == nil
		if waitErr != nil {
			m.connectError = waitErr.Error()
			m.phase = feishuPhaseError
			m.statusMessage = "飞书授权失败，请重新开始连接"
		} else {
			m.phase = feishuPhaseConnected
			m.statusMessage = "应用已授权，请选择机器人所在群聊"
		}
		m.connectCmd = nil
		m.mu.Unlock()
	}()

	response.Success(c, gin.H{"message": "connect session started", "profile_dir": m.profileDir})
}

func feishuConnectStatus(c *gin.Context) {
	m := globalFeishu
	m.mu.Lock()
	defer m.mu.Unlock()
	response.Success(c, gin.H{
		"connecting":       m.connecting,
		"done":             m.connectDone,
		"phase":            m.phase,
		"status_message":   m.statusMessage,
		"verification_url": m.verificationURL,
		"error":            m.connectError,
	})
}

func feishuConnectCancel(c *gin.Context) {
	m := globalFeishu
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectCmd != nil && m.connectCmd.Process != nil {
		_ = m.connectCmd.Process.Kill()
	}
	m.connectCmd = nil
	m.connecting = false
	m.connectDone = false
	m.verificationURL = ""
	m.connectError = ""
	m.phase = feishuPhaseIdle
	m.statusMessage = "连接已取消"
	response.Success(c, gin.H{"cancelled": true})
}

func feishuStatus(c *gin.Context) {
	m := globalFeishu
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	out, err := m.runCLI(ctx, "doctor")
	doctorOK := err == nil

	configOut, _ := m.runCLI(ctx, "config", "show")
	m.loadTarget()

	m.mu.Lock()
	chatID, userID := m.chatID, m.userID
	if doctorOK {
		if chatID != "" || userID != "" {
			m.phase = feishuPhaseReady
			m.statusMessage = "已连接并配置告警目标，可以发送测试消息"
		} else if !m.connecting {
			m.phase = feishuPhaseConnected
			m.statusMessage = "应用已授权，请选择机器人所在群聊"
		}
	} else if !m.connecting {
		m.phase = feishuPhaseIdle
		m.statusMessage = "尚未完成飞书应用授权"
	}
	phase, statusMessage := m.phase, m.statusMessage
	m.mu.Unlock()

	response.Success(c, gin.H{
		"connected":         doctorOK,
		"doctor":            strings.TrimSpace(out),
		"config":            strings.TrimSpace(configOut),
		"chat_id":           chatID,
		"user_id":           userID,
		"phase":             phase,
		"status_message":    statusMessage,
		"target_configured": chatID != "" || userID != "",
	})
}

type feishuChat struct {
	ChatID     string `json:"chat_id"`
	Name       string `json:"name"`
	ChatMode   string `json:"chat_mode"`
	ChatStatus string `json:"chat_status"`
}

func feishuChats(c *gin.Context) {
	m := globalFeishu
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	out, err := m.runCLI(ctx, "im", "+chat-list", "--as", "bot", "--page-size", "100", "--format", "json")
	if err != nil {
		response.InternalError(c, fmt.Sprintf("list chats failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	var payload struct {
		Data struct {
			Chats []feishuChat `json:"chats"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		response.InternalError(c, "invalid chat list response: "+err.Error())
		return
	}
	response.Success(c, gin.H{"chats": payload.Data.Chats})
}

type feishuTargetRequest struct {
	ChatID string `json:"chat_id"`
	UserID string `json:"user_id"`
}

func feishuSetTarget(c *gin.Context) {
	var req feishuTargetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ChatID == "" && req.UserID == "" {
		response.BadRequest(c, "chat_id or user_id required")
		return
	}
	m := globalFeishu
	if err := m.saveTarget(req); err != nil {
		response.InternalError(c, "save target failed: "+err.Error())
		return
	}
	m.mu.Lock()
	m.chatID = req.ChatID
	m.userID = req.UserID
	m.phase = feishuPhaseReady
	m.statusMessage = "告警目标已保存，可以发送测试消息"
	m.mu.Unlock()
	response.Success(c, gin.H{"chat_id": req.ChatID, "user_id": req.UserID})
}

type feishuSendRequest struct {
	Markdown string `json:"markdown"`
	ChatID   string `json:"chat_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

func feishuSendTest(c *gin.Context) {
	var req feishuSendRequest
	_ = c.ShouldBindJSON(&req)
	if req.Markdown == "" {
		req.Markdown = "**Sub2API 飞书通知测试**\n\n如果你看到这条消息，说明通知通道已连通。"
	}
	doFeishuSend(c, req)
}

func feishuSend(c *gin.Context) {
	var req feishuSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if req.Markdown == "" {
		response.BadRequest(c, "markdown content required")
		return
	}
	doFeishuSend(c, req)
}

func doFeishuSend(c *gin.Context, req feishuSendRequest) {
	m := globalFeishu
	m.loadTarget()
	m.mu.Lock()
	chatID := req.ChatID
	if chatID == "" {
		chatID = m.chatID
	}
	userID := req.UserID
	if userID == "" {
		userID = m.userID
	}
	m.mu.Unlock()

	if chatID == "" && userID == "" {
		response.BadRequest(c, "no target configured; set chat_id or user_id first")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	args := []string{"im", "+messages-send", "--as", "bot", "--markdown", req.Markdown}
	if chatID != "" {
		args = append(args, "--chat-id", chatID)
	} else {
		args = append(args, "--user-id", userID)
	}

	out, err := m.runCLI(ctx, args...)
	if err != nil {
		response.InternalError(c, fmt.Sprintf("send failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	response.Success(c, gin.H{"sent": true, "output": strings.TrimSpace(out)})
}

// ===========================================================================
// 上游监控（Sub2API adapter + 采集 + 告警）
// ===========================================================================

type upstreamMonitorTarget struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Type              string  `json:"type"` // "sub2api"
	BaseURL           string  `json:"base_url"`
	RefreshToken      string  `json:"refresh_token"`
	APIKey            string  `json:"api_key,omitempty"`
	BalanceThreshold  float64 `json:"balance_threshold"`
	RateChangePercent float64 `json:"rate_change_percent"`
	Enabled           bool    `json:"enabled"`
}

type upstreamSnapshot struct {
	TargetID    string          `json:"target_id"`
	CollectedAt time.Time       `json:"collected_at"`
	Balance     float64         `json:"balance"`
	Currency    string          `json:"currency"`
	Groups      []upstreamGroup `json:"groups"`
	AuthValid   bool            `json:"auth_valid"`
	Error       string          `json:"error,omitempty"`
}

type upstreamGroup struct {
	ID   int64   `json:"id,omitempty"`
	Name string  `json:"name"`
	Rate float64 `json:"rate"`
}

type upstreamAlert struct {
	TargetID    string    `json:"target_id"`
	TargetName  string    `json:"target_name"`
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	Message     string    `json:"message"`
	Detail      string    `json:"detail,omitempty"`
	TriggeredAt time.Time `json:"triggered_at"`
}

type upstreamMonitorManager struct {
	mu sync.Mutex

	running       bool
	intervalSecs  int
	targets       []upstreamMonitorTarget
	stopCh        chan struct{}
	db            *sql.DB
	alertDedup    map[string]time.Time
	lastCollectAt time.Time
	lastError     string
	targetStatus  map[string]gin.H
}

var globalUpstreamMonitor = &upstreamMonitorManager{
	intervalSecs: 120,
	alertDedup:   make(map[string]time.Time),
	targetStatus: make(map[string]gin.H),
}

func (m *upstreamMonitorManager) start(intervalSecs int, targets []upstreamMonitorTarget, dbPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		close(m.stopCh)
		time.Sleep(50 * time.Millisecond)
	}

	if intervalSecs < 30 {
		intervalSecs = 30
	}

	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if err := umMigrate(db); err != nil {
		db.Close()
		return err
	}

	m.intervalSecs = intervalSecs
	m.targets = targets
	m.db = db
	m.stopCh = make(chan struct{})
	m.running = true
	m.lastError = ""
	m.alertDedup = make(map[string]time.Time)

	go m.loop(m.stopCh)
	return nil
}

func (m *upstreamMonitorManager) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	close(m.stopCh)
	m.running = false
	if m.db != nil {
		m.db.Close()
		m.db = nil
	}
}

func (m *upstreamMonitorManager) loop(stopCh <-chan struct{}) {
	m.collectAll()
	ticker := time.NewTicker(time.Duration(m.intervalSecs) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			m.collectAll()
		}
	}
}

func (m *upstreamMonitorManager) collectAll() {
	m.mu.Lock()
	targets := make([]upstreamMonitorTarget, 0)
	for _, t := range m.targets {
		if t.Enabled {
			targets = append(targets, t)
		}
	}
	db := m.db
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var firstErr string
	var alerts []upstreamAlert
	for _, target := range targets {
		snap := umCollectTarget(ctx, target)
		if snap.Error != "" && firstErr == "" {
			firstErr = snap.Error
		}
		alerts = append(alerts, m.evaluateAlerts(target, snap, db)...)
		if db != nil {
			umSaveSnapshot(db, snap)
		}

		m.mu.Lock()
		m.targetStatus[target.ID] = gin.H{
			"target_id": target.ID, "name": target.Name,
			"balance": snap.Balance, "auth_valid": snap.AuthValid,
			"last_collect_at": snap.CollectedAt.Format(time.RFC3339),
			"error":           snap.Error, "group_count": len(snap.Groups),
		}
		m.mu.Unlock()
	}

	// Dispatch alerts through the selected notification provider.
	if len(alerts) > 0 {
		umDispatchNotifications(ctx, alerts)
		if db != nil {
			for i := range alerts {
				umSaveAlert(db, &alerts[i])
			}
		}
	}

	m.mu.Lock()
	m.lastCollectAt = time.Now()
	m.lastError = firstErr
	m.mu.Unlock()
}

func (m *upstreamMonitorManager) evaluateAlerts(target upstreamMonitorTarget, snap *upstreamSnapshot, db *sql.DB) []upstreamAlert {
	var alerts []upstreamAlert
	now := time.Now()

	if !snap.AuthValid && snap.Error != "" {
		if m.shouldAlert(target.ID+":auth_failure", 5*time.Minute) {
			alerts = append(alerts, upstreamAlert{
				TargetID: target.ID, TargetName: target.Name,
				Type: "auth_failure", Severity: "critical",
				Message: "认证失败，监控失明", Detail: snap.Error, TriggeredAt: now,
			})
		}
		return alerts
	}

	if target.BalanceThreshold > 0 && snap.Balance < target.BalanceThreshold {
		if m.shouldAlert(target.ID+":low_balance", 10*time.Minute) {
			alerts = append(alerts, upstreamAlert{
				TargetID: target.ID, TargetName: target.Name,
				Type: "low_balance", Severity: "warning",
				Message:     "余额低于阈值",
				Detail:      fmt.Sprintf("balance=%.4f threshold=%.4f", snap.Balance, target.BalanceThreshold),
				TriggeredAt: now,
			})
		}
	}

	if db != nil && target.RateChangePercent > 0 {
		prev := umLatestSnapshot(db, target.ID)
		if prev != nil && len(prev.Groups) > 0 {
			changes := umDiffRates(prev.Groups, snap.Groups, target.RateChangePercent)
			if changes != "" && m.shouldAlert(target.ID+":rate_change", 15*time.Minute) {
				alerts = append(alerts, upstreamAlert{
					TargetID: target.ID, TargetName: target.Name,
					Type: "rate_change", Severity: "warning",
					Message: "分组倍率变更", Detail: changes, TriggeredAt: now,
				})
			}
			if umGroupSetChanged(prev.Groups, snap.Groups) && m.shouldAlert(target.ID+":group_change", 15*time.Minute) {
				alerts = append(alerts, upstreamAlert{
					TargetID: target.ID, TargetName: target.Name,
					Type: "group_change", Severity: "info",
					Message: "分组集合变更", Detail: umDiffGroupSets(prev.Groups, snap.Groups), TriggeredAt: now,
				})
			}
		}
	}
	return alerts
}

func (m *upstreamMonitorManager) shouldAlert(key string, cooldown time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if last, ok := m.alertDedup[key]; ok && time.Since(last) < cooldown {
		return false
	}
	m.alertDedup[key] = time.Now()
	return true
}

// --- Sub2API Adapter ---

func umCollectTarget(ctx context.Context, target upstreamMonitorTarget) *upstreamSnapshot {
	snap := &upstreamSnapshot{TargetID: target.ID, CollectedAt: time.Now(), AuthValid: true}

	// Refresh token → access token
	token, err := umRefreshToken(ctx, target)
	if err != nil {
		snap.AuthValid = false
		snap.Error = "auth: " + err.Error()
		return snap
	}

	// Profile (balance)
	balance, currency, err := umFetchProfile(ctx, target.BaseURL, token)
	if err != nil {
		snap.Error = "profile: " + err.Error()
		return snap
	}
	snap.Balance = balance
	snap.Currency = currency

	// Group rates
	groups, err := umFetchGroupRates(ctx, target.BaseURL, token)
	if err != nil {
		snap.Error = "rates: " + err.Error()
	} else {
		snap.Groups = groups
	}
	return snap
}

func umRefreshToken(ctx context.Context, target upstreamMonitorTarget) (string, error) {
	if target.RefreshToken == "" {
		return "", errors.New("no refresh_token")
	}
	url := strings.TrimRight(target.BaseURL, "/") + "/api/v1/auth/refresh"
	body := fmt.Sprintf(`{"refresh_token":%q}`, target.RefreshToken)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("status %d: %.200s", resp.StatusCode, data)
	}

	var result struct {
		Data struct {
			AccessToken string `json:"access_token"`
			AuthToken   string `json:"auth_token"`
			Token       string `json:"token"`
		} `json:"data"`
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(data, &result)
	token := result.Data.AccessToken
	if token == "" {
		token = result.Data.AuthToken
	}
	if token == "" {
		token = result.Data.Token
	}
	if token == "" {
		token = result.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("no token in response: %.200s", data)
	}
	return token, nil
}

func umFetchProfile(ctx context.Context, baseURL, token string) (float64, string, error) {
	url := strings.TrimRight(baseURL, "/") + "/api/v1/user/profile"
	data, err := umAuthGet(ctx, url, token)
	if err != nil {
		return 0, "", err
	}
	var result struct {
		Data struct {
			Balance  float64 `json:"balance"`
			Quota    float64 `json:"quota"`
			Currency string  `json:"currency"`
		} `json:"data"`
		Balance float64 `json:"balance"`
	}
	_ = json.Unmarshal(data, &result)
	balance := result.Data.Balance
	if balance == 0 {
		balance = result.Balance
	}
	if balance == 0 {
		balance = result.Data.Quota
	}
	currency := result.Data.Currency
	if currency == "" {
		currency = "USD"
	}
	return balance, currency, nil
}

func umFetchGroupRates(ctx context.Context, baseURL, token string) ([]upstreamGroup, error) {
	base := strings.TrimRight(baseURL, "/")
	data, err := umAuthGet(ctx, base+"/api/v1/groups/available", token)
	if err != nil {
		return nil, err
	}
	groups, err := umParseGroups(data)
	if err != nil {
		return nil, err
	}

	// /groups/rates contains user-specific multipliers keyed by group ID. The
	// available-groups response remains the source of names and default rates.
	ratesData, err := umAuthGet(ctx, base+"/api/v1/groups/rates", token)
	if err != nil {
		return groups, nil
	}
	var ratesResp struct {
		Data map[string]float64 `json:"data"`
	}
	if json.Unmarshal(ratesData, &ratesResp) == nil && len(ratesResp.Data) > 0 {
		for i := range groups {
			if rate, ok := ratesResp.Data[strconv.FormatInt(groups[i].ID, 10)]; ok {
				groups[i].Rate = rate
			} else if rate, ok := ratesResp.Data[groups[i].Name]; ok {
				// Keep compatibility with deployments that key rates by name.
				groups[i].Rate = rate
			}
		}
	}
	return groups, nil
}

func umParseGroups(data []byte) ([]upstreamGroup, error) {
	// Format: {"data": [{"name":"...","ratio":1.0}]}
	var listResp struct {
		Data []struct {
			ID             int64   `json:"id"`
			Name           string  `json:"name"`
			Ratio          float64 `json:"ratio"`
			Rate           float64 `json:"rate"`
			RateMultiplier float64 `json:"rate_multiplier"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &listResp) == nil && len(listResp.Data) > 0 {
		groups := make([]upstreamGroup, 0, len(listResp.Data))
		for _, g := range listResp.Data {
			r := g.Ratio
			if r == 0 {
				r = g.Rate
			}
			if r == 0 {
				r = g.RateMultiplier
			}
			groups = append(groups, upstreamGroup{ID: g.ID, Name: g.Name, Rate: r})
		}
		return groups, nil
	}
	// Format: {"data": {"name": ratio}}
	var mapResp struct {
		Data map[string]float64 `json:"data"`
	}
	if json.Unmarshal(data, &mapResp) == nil && len(mapResp.Data) > 0 {
		groups := make([]upstreamGroup, 0, len(mapResp.Data))
		for name, rate := range mapResp.Data {
			groups = append(groups, upstreamGroup{Name: name, Rate: rate})
		}
		return groups, nil
	}
	return nil, fmt.Errorf("unrecognized format: %.200s", data)
}

func umAuthGet(ctx context.Context, url, token string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %.200s", resp.StatusCode, data)
	}
	return data, nil
}

// --- Alert dispatch via feishu (lark-cli) ---

func umDispatchFeishu(ctx context.Context, alerts []upstreamAlert) {
	m := globalFeishu
	m.mu.Lock()
	chatID, userID := m.chatID, m.userID
	m.mu.Unlock()
	if chatID == "" && userID == "" {
		return // no target configured
	}

	var sb strings.Builder
	for i, a := range alerts {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		icon := "⚠️"
		if a.Severity == "critical" {
			icon = "🔴"
		} else if a.Severity == "info" {
			icon = "🔵"
		}
		sb.WriteString(fmt.Sprintf("%s **[%s] %s**\n", icon, a.TargetName, a.Message))
		if a.Detail != "" {
			sb.WriteString(a.Detail + "\n")
		}
		sb.WriteString(a.TriggeredAt.Format("2006-01-02 15:04:05"))
	}

	args := []string{"im", "+messages-send", "--as", "bot", "--markdown", sb.String()}
	if chatID != "" {
		args = append(args, "--chat-id", chatID)
	} else {
		args = append(args, "--user-id", userID)
	}
	_, _ = m.runCLI(ctx, args...)
}

// --- SQLite storage ---

func umMigrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS um_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    balance REAL NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    groups_json TEXT NOT NULL DEFAULT '[]',
    auth_valid INTEGER NOT NULL DEFAULT 1,
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_um_snap_target ON um_snapshots(target_id, collected_at DESC);
CREATE TABLE IF NOT EXISTS um_alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target_id TEXT NOT NULL,
    target_name TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'warning',
    message TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    triggered_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_um_alerts_time ON um_alerts(triggered_at DESC);
`)
	return err
}

func umSaveSnapshot(db *sql.DB, snap *upstreamSnapshot) {
	gj, _ := json.Marshal(snap.Groups)
	av := 0
	if snap.AuthValid {
		av = 1
	}
	_, _ = db.Exec(`INSERT INTO um_snapshots (target_id, collected_at, balance, currency, groups_json, auth_valid, error) VALUES (?,?,?,?,?,?,?)`,
		snap.TargetID, snap.CollectedAt.UTC().Format(time.RFC3339), snap.Balance, snap.Currency, string(gj), av, snap.Error)
}

func umLatestSnapshot(db *sql.DB, targetID string) *upstreamSnapshot {
	row := db.QueryRow(`SELECT target_id, collected_at, balance, currency, groups_json, auth_valid, error FROM um_snapshots WHERE target_id=? ORDER BY collected_at DESC LIMIT 1`, targetID)
	var snap upstreamSnapshot
	var ca, gj string
	var av int
	if row.Scan(&snap.TargetID, &ca, &snap.Balance, &snap.Currency, &gj, &av, &snap.Error) != nil {
		return nil
	}
	snap.CollectedAt, _ = time.Parse(time.RFC3339, ca)
	snap.AuthValid = av == 1
	_ = json.Unmarshal([]byte(gj), &snap.Groups)
	return &snap
}

func umSaveAlert(db *sql.DB, a *upstreamAlert) {
	_, _ = db.Exec(`INSERT INTO um_alerts (target_id, target_name, type, severity, message, detail, triggered_at) VALUES (?,?,?,?,?,?,?)`,
		a.TargetID, a.TargetName, a.Type, a.Severity, a.Message, a.Detail, a.TriggeredAt.UTC().Format(time.RFC3339))
}

// --- Diff helpers ---

func umDiffRates(prev, curr []upstreamGroup, thresholdPct float64) string {
	prevMap := make(map[string]float64, len(prev))
	for _, g := range prev {
		prevMap[g.Name] = g.Rate
	}
	var changes []string
	for _, g := range curr {
		old, ok := prevMap[g.Name]
		if !ok || old == 0 {
			continue
		}
		pct := (g.Rate - old) / old * 100
		if pct < 0 {
			pct = -pct
		}
		if pct >= thresholdPct {
			changes = append(changes, fmt.Sprintf("%s: %.4f→%.4f(%.1f%%)", g.Name, old, g.Rate, pct))
		}
	}
	return strings.Join(changes, "; ")
}

func umGroupSetChanged(prev, curr []upstreamGroup) bool {
	if len(prev) != len(curr) {
		return true
	}
	set := make(map[string]struct{}, len(prev))
	for _, g := range prev {
		set[g.Name] = struct{}{}
	}
	for _, g := range curr {
		if _, ok := set[g.Name]; !ok {
			return true
		}
	}
	return false
}

func umDiffGroupSets(prev, curr []upstreamGroup) string {
	prevSet := make(map[string]struct{}, len(prev))
	for _, g := range prev {
		prevSet[g.Name] = struct{}{}
	}
	currSet := make(map[string]struct{}, len(curr))
	for _, g := range curr {
		currSet[g.Name] = struct{}{}
	}
	var parts []string
	for name := range currSet {
		if _, ok := prevSet[name]; !ok {
			parts = append(parts, "+"+name)
		}
	}
	for name := range prevSet {
		if _, ok := currSet[name]; !ok {
			parts = append(parts, "-"+name)
		}
	}
	return strings.Join(parts, ", ")
}

// --- Monitor Route Handlers ---

type upstreamMonitorStartRequest struct {
	IntervalSeconds int                     `json:"interval_seconds"`
	Targets         []upstreamMonitorTarget `json:"targets"`
	DBPath          string                  `json:"db_path"`
}

func upstreamMonitorStart(c *gin.Context) {
	var req upstreamMonitorStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if len(req.Targets) == 0 {
		response.BadRequest(c, "at least one target required")
		return
	}
	for i := range req.Targets {
		t := &req.Targets[i]
		if t.ID == "" {
			t.ID = t.Name
		}
		if t.BaseURL == "" {
			response.BadRequest(c, "target base_url required: "+t.Name)
			return
		}
		if t.RefreshToken == "" && t.APIKey == "" {
			response.BadRequest(c, "target needs refresh_token or api_key: "+t.Name)
			return
		}
		if t.BalanceThreshold == 0 {
			t.BalanceThreshold = 1.0
		}
		if t.RateChangePercent == 0 {
			t.RateChangePercent = 10.0
		}
		t.Enabled = true
	}
	dbPath := req.DBPath
	if dbPath == "" {
		dbPath = "./data/upstream_monitor.db"
	}
	if err := globalUpstreamMonitor.start(req.IntervalSeconds, req.Targets, dbPath); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"running": true, "targets": len(req.Targets)})
}

func upstreamMonitorStop(c *gin.Context) {
	globalUpstreamMonitor.stop()
	response.Success(c, gin.H{"running": false})
}

func upstreamMonitorStatus(c *gin.Context) {
	m := globalUpstreamMonitor
	m.mu.Lock()
	statuses := make([]gin.H, 0, len(m.targetStatus))
	for _, s := range m.targetStatus {
		statuses = append(statuses, s)
	}
	resp := gin.H{
		"running":         m.running,
		"interval_secs":   m.intervalSecs,
		"last_collect_at": m.lastCollectAt.Format(time.RFC3339),
		"last_error":      m.lastError,
		"targets":         statuses,
	}
	m.mu.Unlock()
	response.Success(c, resp)
}

func upstreamMonitorCollectNow(c *gin.Context) {
	m := globalUpstreamMonitor
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if !running {
		response.BadRequest(c, "monitor not running")
		return
	}
	go m.collectAll()
	response.Success(c, gin.H{"message": "collection triggered"})
}

func upstreamMonitorSnapshots(c *gin.Context) {
	m := globalUpstreamMonitor
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	if db == nil {
		response.BadRequest(c, "monitor not running")
		return
	}
	targetID := c.Query("target_id")
	limit := 20
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	var rows *sql.Rows
	var err error
	if targetID != "" {
		rows, err = db.Query(`SELECT target_id, collected_at, balance, currency, groups_json, auth_valid, error FROM um_snapshots WHERE target_id=? ORDER BY collected_at DESC LIMIT ?`, targetID, limit)
	} else {
		rows, err = db.Query(`SELECT target_id, collected_at, balance, currency, groups_json, auth_valid, error FROM um_snapshots ORDER BY collected_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	defer rows.Close()
	var snaps []gin.H
	for rows.Next() {
		var tid, ca, cur, gj, e string
		var bal float64
		var av int
		if rows.Scan(&tid, &ca, &bal, &cur, &gj, &av, &e) == nil {
			snaps = append(snaps, gin.H{"target_id": tid, "collected_at": ca, "balance": bal, "currency": cur, "groups": gj, "auth_valid": av == 1, "error": e})
		}
	}
	response.Success(c, snaps)
}

func upstreamMonitorAlerts(c *gin.Context) {
	m := globalUpstreamMonitor
	m.mu.Lock()
	db := m.db
	m.mu.Unlock()
	if db == nil {
		response.BadRequest(c, "monitor not running")
		return
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := db.Query(`SELECT id, target_id, target_name, type, severity, message, detail, triggered_at FROM um_alerts ORDER BY triggered_at DESC LIMIT ?`, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	defer rows.Close()
	var alerts []gin.H
	for rows.Next() {
		var id int64
		var tid, tn, tp, sev, msg, det, ta string
		if rows.Scan(&id, &tid, &tn, &tp, &sev, &msg, &det, &ta) == nil {
			alerts = append(alerts, gin.H{"id": id, "target_id": tid, "target_name": tn, "type": tp, "severity": sev, "message": msg, "detail": det, "triggered_at": ta})
		}
	}
	response.Success(c, alerts)
}

const (
	notificationConfigPath = "/app/data/notification-config.json"
	dingtalkTargetPath     = "/app/data/dws-profile/target.json"
	dingtalkProfileDir     = "/app/data/dws-profile"
	wechatProfileDir       = "/app/data/wxclawbot-profile"
	wechatStateDir         = wechatProfileDir + "/openclaw-weixin"
	wechatAccountsDir      = wechatStateDir + "/accounts"
	wechatAPIBaseURL       = "https://ilinkai.weixin.qq.com"
	wechatBotType          = "3"
	wechatClientVersion    = "132102" // iLink protocol 2.4.6
)

type notificationProvider string

const (
	notificationProviderFeishu   notificationProvider = "feishu"
	notificationProviderDingTalk notificationProvider = "dingtalk"
	notificationProviderWeChat   notificationProvider = "wechat"
	notificationProviderClaw163  notificationProvider = "claw163"
)

type notificationConfig struct {
	Provider notificationProvider `json:"provider"`
}

func isNotificationProvider(provider notificationProvider) bool {
	if provider == notificationProviderClaw163 {
		return true
	}
	return provider == notificationProviderFeishu || provider == notificationProviderDingTalk || provider == notificationProviderWeChat
}

type notificationConfigState struct {
	mu       sync.Mutex
	provider notificationProvider
}

var globalNotificationConfig = &notificationConfigState{provider: notificationProviderFeishu}

func init() {
	if data, err := os.ReadFile(notificationConfigPath); err == nil {
		var saved notificationConfig
		if json.Unmarshal(data, &saved) == nil && isNotificationProvider(saved.Provider) {
			globalNotificationConfig.provider = saved.Provider
		}
	}
}

func currentNotificationProvider() notificationProvider {
	globalNotificationConfig.mu.Lock()
	defer globalNotificationConfig.mu.Unlock()
	return globalNotificationConfig.provider
}

func saveNotificationProvider(provider notificationProvider) error {
	if !isNotificationProvider(provider) {
		return errors.New("unsupported notification provider")
	}
	if err := os.MkdirAll(filepath.Dir(notificationConfigPath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(notificationConfig{Provider: provider})
	if err != nil {
		return err
	}
	tmpPath := notificationConfigPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, notificationConfigPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	globalNotificationConfig.mu.Lock()
	globalNotificationConfig.provider = provider
	globalNotificationConfig.mu.Unlock()
	return nil
}

func notificationProviders(c *gin.Context) {
	response.Success(c, gin.H{
		"selected": currentNotificationProvider(),
		"providers": []gin.H{
			{"id": notificationProviderFeishu, "name": "飞书", "available": true},
			{"id": notificationProviderDingTalk, "name": "钉钉（dws CLI）", "available": true},
			{"id": notificationProviderWeChat, "name": "微信", "available": true},
			{"id": notificationProviderClaw163, "name": "Claw163 邮箱（CLI）", "available": true},
		},
	})
}

func notificationSetProvider(c *gin.Context) {
	var req struct {
		Provider notificationProvider `json:"provider"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := saveNotificationProvider(req.Provider); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"provider": req.Provider})
}

// ===========================================================================
// 微信通知通道（wxclawbot-cli 发送 + iLink QR 授权）
// ===========================================================================

type wechatManager struct {
	mu sync.Mutex

	cliPath          string
	connecting       bool
	connectDone      bool
	connectCancel    context.CancelFunc
	connectError     string
	verificationURL  string
	qrcode           string
	phase            string
	statusMessage    string
	updatesRunning   bool
	updatesCancel    context.CancelFunc
	rateLimitedUntil time.Time

	accountID string
	botToken  string
	baseURL   string
	userID    string
}

const (
	wechatPhaseIdle                  = "idle"
	wechatPhaseStarting              = "starting"
	wechatPhaseAwaitingAuthorization = "awaiting_authorization"
	wechatPhaseConnected             = "connected"
	wechatPhaseReady                 = "ready"
	wechatPhaseError                 = "error"
)

var globalWeChat = newWeChatManager()

func newWeChatManager() *wechatManager {
	m := &wechatManager{baseURL: wechatAPIBaseURL, phase: wechatPhaseIdle}
	m.loadAccount()
	m.ensureUpdatesMonitor()
	return m
}

func (m *wechatManager) cliEnv() []string {
	env := os.Environ()
	values := map[string]string{
		"HOME":               wechatProfileDir,
		"OPENCLAW_STATE_DIR": wechatProfileDir,
		"CLAWDBOT_STATE_DIR": wechatProfileDir,
	}
	for key, value := range values {
		prefix := key + "="
		found := false
		for i, item := range env {
			if strings.HasPrefix(item, prefix) {
				env[i] = prefix + value
				found = true
				break
			}
		}
		if !found {
			env = append(env, prefix+value)
		}
	}
	return env
}

func (m *wechatManager) resolveCLI() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cliPath != "" {
		if _, err := os.Stat(m.cliPath); err == nil {
			return m.cliPath, nil
		}
		m.cliPath = ""
	}
	for _, candidate := range []string{os.Getenv("WXCLAWBOT_CLI_PATH"), "/usr/local/bin/wxclawbot"} {
		if candidate == "" {
			continue
		}
		if filepath.Base(candidate) == candidate {
			if path, err := exec.LookPath(candidate); err == nil {
				m.cliPath = path
				return path, nil
			}
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			m.cliPath = candidate
			return candidate, nil
		}
	}
	return "", errors.New("wxclawbot is not installed; rebuild the Sub2API image with @claw-lab/wxclawbot-cli")
}

func (m *wechatManager) runCLI(ctx context.Context, args ...string) (string, error) {
	cli, err := m.resolveCLI()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(wechatProfileDir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Env = m.cliEnv()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (m *wechatManager) loadAccount() {
	data, err := os.ReadFile(filepath.Join(wechatStateDir, "accounts.json"))
	if err != nil {
		return
	}
	var accountIDs []string
	if json.Unmarshal(data, &accountIDs) != nil {
		return
	}
	for _, accountID := range accountIDs {
		accountID = strings.TrimSpace(accountID)
		if accountID == "" || strings.ContainsAny(accountID, `/\\`) {
			continue
		}
		data, err = os.ReadFile(filepath.Join(wechatAccountsDir, accountID+".json"))
		if err != nil {
			continue
		}
		var account struct {
			Token   string `json:"token"`
			BaseURL string `json:"baseUrl"`
			UserID  string `json:"userId"`
		}
		if json.Unmarshal(data, &account) != nil || strings.TrimSpace(account.Token) == "" {
			continue
		}
		m.mu.Lock()
		m.accountID = accountID
		m.botToken = strings.TrimSpace(account.Token)
		m.baseURL = strings.TrimSpace(account.BaseURL)
		if m.baseURL == "" {
			m.baseURL = wechatAPIBaseURL
		}
		m.userID = strings.TrimSpace(account.UserID)
		m.mu.Unlock()
		return
	}
}

func saveWechatAccount(accountID, token, baseURL, userID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || strings.ContainsAny(accountID, `/\\`) || strings.Contains(accountID, "..") {
		return errors.New("微信授权返回了无效的账号 ID")
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("微信授权未返回 bot token")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = wechatAPIBaseURL
	}
	if err := os.MkdirAll(wechatAccountsDir, 0o755); err != nil {
		return err
	}
	accountData, err := json.Marshal(map[string]string{
		"token":   strings.TrimSpace(token),
		"savedAt": time.Now().UTC().Format(time.RFC3339),
		"baseUrl": strings.TrimSpace(baseURL),
		"userId":  strings.TrimSpace(userID),
	})
	if err != nil {
		return err
	}
	accountPath := filepath.Join(wechatAccountsDir, accountID+".json")
	if err := os.WriteFile(accountPath+".tmp", accountData, 0o600); err != nil {
		return err
	}
	if err := os.Rename(accountPath+".tmp", accountPath); err != nil {
		_ = os.Remove(accountPath + ".tmp")
		return err
	}

	indexPath := filepath.Join(wechatStateDir, "accounts.json")
	var accountIDs []string
	if data, readErr := os.ReadFile(indexPath); readErr == nil {
		_ = json.Unmarshal(data, &accountIDs)
	}
	found := false
	for _, id := range accountIDs {
		if id == accountID {
			found = true
			break
		}
	}
	if !found {
		accountIDs = append(accountIDs, accountID)
	}
	indexData, err := json.Marshal(accountIDs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(indexPath+".tmp", indexData, 0o600); err != nil {
		return err
	}
	if err := os.Rename(indexPath+".tmp", indexPath); err != nil {
		_ = os.Remove(indexPath + ".tmp")
		return err
	}
	return nil
}

func wechatContextTokenPath(accountID string) string {
	return filepath.Join(wechatAccountsDir, accountID+".context-tokens.json")
}

func wechatSyncBufferPath(accountID string) string {
	return filepath.Join(wechatAccountsDir, accountID+".sync.json")
}

func wechatHasContextToken(accountID, userID string) bool {
	if accountID == "" || userID == "" {
		return false
	}
	data, err := os.ReadFile(wechatContextTokenPath(accountID))
	if err != nil {
		return false
	}
	var tokens map[string]string
	if json.Unmarshal(data, &tokens) != nil {
		return false
	}
	return strings.TrimSpace(tokens[userID]) != ""
}

func saveWechatContextToken(accountID, userID, token string) error {
	if accountID == "" || userID == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	path := wechatContextTokenPath(accountID)
	tokens := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &tokens)
	}
	tokens[userID] = strings.TrimSpace(token)
	data, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(wechatAccountsDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		_ = os.Remove(path + ".tmp")
		return err
	}
	return nil
}

type wechatQRCodeResponse struct {
	QRCode         string `json:"qrcode"`
	QRCodeImageURL string `json:"qrcode_img_content"`
}

type wechatQRCodeStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	BotID        string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	UserID       string `json:"ilink_user_id"`
	RedirectHost string `json:"redirect_host"`
}

func wechatHTTP(ctx context.Context, method, endpoint string, body []byte, timeout time.Duration, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", wechatClientVersion)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	var random [4]byte
	if _, err := cryptorand.Read(random[:]); err == nil {
		req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(random[0])<<24|uint64(random[1])<<16|uint64(random[2])<<8|uint64(random[3]), 10))))
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("微信接口返回 status %d: %.200s", resp.StatusCode, data)
	}
	return data, nil
}

func (m *wechatManager) fetchQRCode(ctx context.Context) (wechatQRCodeResponse, error) {
	endpoint := wechatAPIBaseURL + "/ilink/bot/get_bot_qrcode?bot_type=" + url.QueryEscape(wechatBotType)
	data, err := wechatHTTP(ctx, http.MethodPost, endpoint, []byte(`{"local_token_list":[]}`), 20*time.Second, "")
	if err != nil {
		return wechatQRCodeResponse{}, err
	}
	var result wechatQRCodeResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.QRCode == "" || result.QRCodeImageURL == "" {
		return result, errors.New("微信接口未返回二维码")
	}
	return result, nil
}

func (m *wechatManager) pollQRCode(ctx context.Context, baseURL, qrcode string) (wechatQRCodeStatusResponse, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(qrcode)
	data, err := wechatHTTP(ctx, http.MethodGet, endpoint, nil, 35*time.Second, "")
	if err != nil {
		return wechatQRCodeStatusResponse{}, err
	}
	var result wechatQRCodeStatusResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	return result, nil
}

type wechatUpdatesResponse struct {
	Ret     int `json:"ret"`
	ErrCode int `json:"errcode"`
	Msgs    []struct {
		FromUserID   string `json:"from_user_id"`
		ContextToken string `json:"context_token"`
	} `json:"msgs"`
	GetUpdatesBuf      string `json:"get_updates_buf"`
	LongPollingTimeout int    `json:"longpolling_timeout_ms"`
}

func (m *wechatManager) ensureUpdatesMonitor() {
	m.mu.Lock()
	if m.updatesRunning || m.accountID == "" || m.botToken == "" || m.userID == "" {
		m.mu.Unlock()
		return
	}
	accountID, token, baseURL, userID := m.accountID, m.botToken, m.baseURL, m.userID
	ctx, cancel := context.WithCancel(context.Background())
	m.updatesRunning = true
	m.updatesCancel = cancel
	m.mu.Unlock()
	go m.monitorUpdates(ctx, accountID, token, baseURL, userID)
}

func (m *wechatManager) monitorUpdates(ctx context.Context, accountID, token, baseURL, userID string) {
	defer func() {
		m.mu.Lock()
		m.updatesRunning = false
		m.updatesCancel = nil
		m.mu.Unlock()
	}()

	syncPath := wechatSyncBufferPath(accountID)
	getUpdatesBuf := ""
	if data, err := os.ReadFile(syncPath); err == nil {
		_ = json.Unmarshal(data, &getUpdatesBuf)
	}
	for {
		body, _ := json.Marshal(map[string]any{
			"get_updates_buf": getUpdatesBuf,
			"base_info": map[string]string{
				"channel_version": "2.4.6",
				"bot_agent":       "wxclawbot/0.5.2",
			},
		})
		endpoint := strings.TrimRight(baseURL, "/") + "/ilink/bot/getupdates"
		data, err := wechatHTTP(ctx, http.MethodPost, endpoint, body, 35*time.Second, token)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		var result wechatUpdatesResponse
		if err == nil && json.Unmarshal(data, &result) == nil {
			if result.GetUpdatesBuf != "" && result.GetUpdatesBuf != getUpdatesBuf {
				getUpdatesBuf = result.GetUpdatesBuf
				if encoded, marshalErr := json.Marshal(getUpdatesBuf); marshalErr == nil {
					_ = os.WriteFile(syncPath+".tmp", encoded, 0o600)
					_ = os.Rename(syncPath+".tmp", syncPath)
				}
			}
			if result.Ret == -14 || result.ErrCode == -14 {
				return
			}
			for _, message := range result.Msgs {
				if strings.TrimSpace(message.FromUserID) == userID && strings.TrimSpace(message.ContextToken) != "" {
					if saveErr := saveWechatContextToken(accountID, userID, message.ContextToken); saveErr == nil {
						m.mu.Lock()
						if m.phase != wechatPhaseError {
							m.phase = wechatPhaseReady
							m.statusMessage = "微信会话已建立，可以发送测试消息"
						}
						m.mu.Unlock()
					}
				}
			}
			if result.LongPollingTimeout > 0 && result.LongPollingTimeout < 60_000 {
				// The server controls the hold duration; the next request still uses
				// the fixed client timeout so cancellation remains responsive.
				_ = result.LongPollingTimeout
			}
		}
	}
}

func (m *wechatManager) setConnectError(message string) {
	m.mu.Lock()
	m.connecting = false
	m.connectDone = false
	m.connectError = message
	m.phase = wechatPhaseError
	m.statusMessage = "微信连接失败，请重新扫码"
	m.connectCancel = nil
	m.mu.Unlock()
}

func (m *wechatManager) runLogin(ctx context.Context) {
	qr, err := m.fetchQRCode(ctx)
	if err != nil {
		if ctx.Err() == nil {
			m.setConnectError("获取微信二维码失败: " + err.Error())
		}
		return
	}
	m.mu.Lock()
	m.qrcode = qr.QRCode
	m.verificationURL = qr.QRCodeImageURL
	m.phase = wechatPhaseAwaitingAuthorization
	m.statusMessage = "请使用微信扫描二维码完成授权"
	m.mu.Unlock()

	baseURL := wechatAPIBaseURL
	refreshes := 0
	for {
		status, pollErr := m.pollQRCode(ctx, baseURL, qr.QRCode)
		if pollErr != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		switch status.Status {
		case "wait":
			continue
		case "scaned":
			m.mu.Lock()
			m.phase = wechatPhaseAwaitingAuthorization
			m.statusMessage = "微信已扫码，正在确认授权"
			m.mu.Unlock()
		case "scaned_but_redirect":
			if status.RedirectHost != "" {
				baseURL = "https://" + strings.TrimSpace(status.RedirectHost)
			}
		case "expired":
			refreshes++
			if refreshes > 3 {
				m.setConnectError("微信二维码已多次过期，请重新开始连接")
				return
			}
			qr, err = m.fetchQRCode(ctx)
			if err != nil {
				m.setConnectError("刷新微信二维码失败: " + err.Error())
				return
			}
			baseURL = wechatAPIBaseURL
			m.mu.Lock()
			m.qrcode = qr.QRCode
			m.verificationURL = qr.QRCodeImageURL
			m.statusMessage = "二维码已刷新，请使用微信重新扫描"
			m.mu.Unlock()
		case "need_verifycode":
			m.setConnectError("微信要求输入验证码，请重新扫码后按微信提示完成验证")
			return
		case "verify_code_blocked":
			m.setConnectError("微信验证码尝试次数过多，请稍后重新扫码")
			return
		case "binded_redirect":
			m.setConnectError("这个微信账号已经绑定，请刷新状态后直接使用")
			return
		case "confirmed":
			botID := strings.TrimSpace(status.BotID)
			if botID == "" {
				m.setConnectError("微信授权未返回机器人账号 ID")
				return
			}
			if err := saveWechatAccount(botID, status.BotToken, status.BaseURL, status.UserID); err != nil {
				m.setConnectError("保存微信授权失败: " + err.Error())
				return
			}
			m.mu.Lock()
			m.accountID = botID
			m.botToken = strings.TrimSpace(status.BotToken)
			m.baseURL = strings.TrimSpace(status.BaseURL)
			if m.baseURL == "" {
				m.baseURL = wechatAPIBaseURL
			}
			m.userID = strings.TrimSpace(status.UserID)
			m.connecting = false
			m.connectDone = true
			m.connectError = ""
			m.phase = wechatPhaseConnected
			m.statusMessage = "微信已授权，可以发送测试消息"
			m.connectCancel = nil
			m.mu.Unlock()
			m.ensureUpdatesMonitor()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func wechatConnectStart(c *gin.Context) {
	m := globalWeChat
	m.mu.Lock()
	if m.connecting {
		m.mu.Unlock()
		response.BadRequest(c, "微信连接已经在进行中")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.connecting = true
	m.connectDone = false
	m.connectCancel = cancel
	m.connectError = ""
	m.verificationURL = ""
	m.qrcode = ""
	m.phase = wechatPhaseStarting
	m.statusMessage = "正在获取微信授权二维码..."
	m.mu.Unlock()
	go m.runLogin(ctx)
	response.Success(c, gin.H{"message": "微信连接已启动"})
}

func wechatConnectStatus(c *gin.Context) {
	m := globalWeChat
	m.mu.Lock()
	defer m.mu.Unlock()
	response.Success(c, gin.H{
		"connecting":       m.connecting,
		"done":             m.connectDone,
		"phase":            m.phase,
		"status_message":   m.statusMessage,
		"verification_url": m.verificationURL,
		"error":            m.connectError,
		"user_id":          m.userID,
	})
}

func wechatConnectCancel(c *gin.Context) {
	m := globalWeChat
	m.mu.Lock()
	cancel := m.connectCancel
	m.connectCancel = nil
	m.connecting = false
	m.connectDone = false
	m.connectError = ""
	m.verificationURL = ""
	m.qrcode = ""
	m.phase = wechatPhaseIdle
	m.statusMessage = "连接已取消"
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	response.Success(c, gin.H{"cancelled": true})
}

func wechatStatus(c *gin.Context) {
	m := globalWeChat
	m.loadAccount()
	m.ensureUpdatesMonitor()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "accounts", "--json")
	connected := false
	if err == nil {
		var accounts []struct {
			Configured bool `json:"configured"`
		}
		if json.Unmarshal([]byte(out), &accounts) == nil {
			for _, account := range accounts {
				if account.Configured {
					connected = true
					break
				}
			}
		}
	}
	m.mu.Lock()
	if connected {
		if wechatHasContextToken(m.accountID, m.userID) {
			m.phase = wechatPhaseReady
			m.statusMessage = "微信会话已建立，可以发送测试消息"
		} else {
			m.phase = wechatPhaseConnected
			m.statusMessage = "请先在微信里给机器人发一条消息，完成会话绑定"
		}
	} else if !m.connecting {
		m.phase = wechatPhaseIdle
		m.statusMessage = "尚未完成微信扫码授权"
	}
	phase, statusMessage, userID, accountID := m.phase, m.statusMessage, m.userID, m.accountID
	m.mu.Unlock()
	response.Success(c, gin.H{
		"connected":         connected,
		"phase":             phase,
		"status_message":    statusMessage,
		"user_id":           userID,
		"target_configured": wechatHasContextToken(accountID, userID),
		"accounts":          strings.TrimSpace(out),
		"error":             errorString(err),
	})
}

type wechatSendRequest struct {
	Text string `json:"text"`
}

func (m *wechatManager) sendText(ctx context.Context, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Sub2API 微信通知测试\n\n如果你看到这条消息，说明通知通道已连通。"
	}
	m.mu.Lock()
	if wait := time.Until(m.rateLimitedUntil); wait > 0 {
		m.mu.Unlock()
		return "", fmt.Errorf("微信接口正在限流，请约 %d 秒后再试", int(wait.Seconds()))
	}
	m.mu.Unlock()
	out, err := m.runCLI(ctx, "send", "--text", text, "--json")
	if err != nil {
		if strings.Contains(out, "ret=-2") || strings.Contains(out, "rate limited") {
			m.mu.Lock()
			m.rateLimitedUntil = time.Now().Add(5 * time.Minute)
			m.mu.Unlock()
			return out, errors.New("微信接口限流（ret=-2），该账号约 5 分钟内发送次数已达上限")
		}
		return out, err
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(out), &result) == nil && !result.OK {
		if strings.Contains(out, "ret=-2") || strings.Contains(out, "rate limited") {
			m.mu.Lock()
			m.rateLimitedUntil = time.Now().Add(5 * time.Minute)
			m.mu.Unlock()
			return out, errors.New("微信接口限流（ret=-2），该账号约 5 分钟内发送次数已达上限")
		}
		if result.Error == "" {
			result.Error = "微信 CLI 返回发送失败"
		}
		return out, errors.New(result.Error)
	}
	return out, nil
}

func wechatSendTest(c *gin.Context) {
	var req wechatSendRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	out, err := globalWeChat.sendText(ctx, req.Text)
	if err != nil {
		response.InternalError(c, fmt.Sprintf("发送微信消息失败: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	response.Success(c, gin.H{"sent": true, "output": strings.TrimSpace(out)})
}

type dingtalkManager struct {
	mu sync.Mutex

	cliPath           string
	connecting        bool
	connectCmd        *exec.Cmd
	connectDone       bool
	connectError      string
	verificationURL   string
	authorizationCode string
	phase             string
	statusMessage     string

	groupID   string
	robotCode string
	robotName string
	appID     string
	appName   string

	createPhase         string
	createTaskID        string
	createError         string
	createStatusMessage string
}

const (
	dingtalkPhaseIdle                  = "idle"
	dingtalkPhaseStarting              = "starting"
	dingtalkPhaseAwaitingAuthorization = "awaiting_authorization"
	dingtalkPhaseConnected             = "connected"
	dingtalkPhaseReady                 = "ready"
	dingtalkPhaseError                 = "error"
	dingtalkCreatePhaseIdle            = "idle"
	dingtalkCreatePhaseSubmitting      = "submitting_robot"
	dingtalkCreatePhaseAwaitingResult  = "awaiting_robot_result"
	dingtalkCreatePhaseCreated         = "robot_created"
	dingtalkCreatePhaseError           = "error"
)

var globalDingTalk = newDingTalkManager()

func newDingTalkManager() *dingtalkManager {
	m := &dingtalkManager{phase: dingtalkPhaseIdle, createPhase: dingtalkCreatePhaseIdle}
	m.loadTarget()
	return m
}

func (m *dingtalkManager) loadTarget() {
	data, err := os.ReadFile(dingtalkTargetPath)
	if err != nil {
		return
	}
	var target struct {
		GroupID      string `json:"group_id"`
		RobotCode    string `json:"robot_code"`
		RobotName    string `json:"robot_name"`
		UnifiedAppID string `json:"unified_app_id"`
		AppName      string `json:"app_name"`
	}
	if json.Unmarshal(data, &target) != nil {
		return
	}
	m.mu.Lock()
	m.groupID = strings.TrimSpace(target.GroupID)
	m.robotCode = strings.TrimSpace(target.RobotCode)
	m.robotName = strings.TrimSpace(target.RobotName)
	m.appID = strings.TrimSpace(target.UnifiedAppID)
	m.appName = strings.TrimSpace(target.AppName)
	m.mu.Unlock()
}

func (m *dingtalkManager) saveTarget(groupID, robotCode string) error {
	if err := os.MkdirAll(dingtalkProfileDir, 0o755); err != nil {
		return err
	}
	m.mu.Lock()
	robotName, appID, appName := m.robotName, m.appID, m.appName
	m.mu.Unlock()
	data, err := json.Marshal(struct {
		GroupID      string `json:"group_id"`
		RobotCode    string `json:"robot_code"`
		RobotName    string `json:"robot_name,omitempty"`
		UnifiedAppID string `json:"unified_app_id,omitempty"`
		AppName      string `json:"app_name,omitempty"`
	}{
		GroupID: groupID, RobotCode: robotCode, RobotName: robotName, UnifiedAppID: appID, AppName: appName,
	})
	if err != nil {
		return err
	}
	tmpPath := dingtalkTargetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dingtalkTargetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (m *dingtalkManager) cliEnv() []string {
	env := os.Environ()
	for i, value := range env {
		if strings.HasPrefix(value, "DWS_CONFIG_DIR=") {
			env[i] = "DWS_CONFIG_DIR=" + dingtalkProfileDir
			return env
		}
	}
	return append(env, "DWS_CONFIG_DIR="+dingtalkProfileDir)
}

func (m *dingtalkManager) resolveCLI() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cliPath != "" {
		if _, err := os.Stat(m.cliPath); err == nil {
			return m.cliPath, nil
		}
		m.cliPath = ""
	}
	candidates := []string{os.Getenv("DWS_CLI_PATH"), "/usr/local/bin/dws", "/app/data/bin/dws"}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if filepath.Base(candidate) == candidate {
			if path, err := exec.LookPath(candidate); err == nil {
				m.cliPath = path
				return path, nil
			}
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			m.cliPath = candidate
			return candidate, nil
		}
	}
	return "", errors.New("dws is not installed; rebuild the Sub2API image with the pinned DingTalk CLI")
}

func (m *dingtalkManager) runCLI(ctx context.Context, args ...string) (string, error) {
	cli, err := m.resolveCLI()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dingtalkProfileDir, 0o755); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Env = m.cliEnv()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func dingtalkConnectStart(c *gin.Context) {
	m := globalDingTalk
	m.mu.Lock()
	if m.connecting {
		m.mu.Unlock()
		response.BadRequest(c, "DingTalk authorization is already in progress")
		return
	}
	m.connecting = true
	m.connectDone = false
	m.connectError = ""
	m.verificationURL = ""
	m.authorizationCode = ""
	m.phase = dingtalkPhaseStarting
	m.statusMessage = "正在启动钉钉设备授权..."
	m.mu.Unlock()

	cli, err := m.resolveCLI()
	if err != nil {
		m.mu.Lock()
		m.connecting = false
		m.phase = dingtalkPhaseError
		m.connectError = err.Error()
		m.statusMessage = "钉钉 CLI 未安装"
		m.mu.Unlock()
		response.InternalError(c, err.Error())
		return
	}
	cmd := exec.Command(cli, "auth", "login", "--device", "--format", "json")
	cmd.Env = m.cliEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.connecting = false
		m.phase = dingtalkPhaseError
		m.connectError = err.Error()
		m.mu.Unlock()
		response.InternalError(c, err.Error())
		return
	}
	m.mu.Lock()
	m.connectCmd = cmd
	m.mu.Unlock()

	go func() {
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 1024)
		for {
			n, readErr := stdout.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
				m.parseAuthOutput(string(buf))
			}
			if readErr != nil {
				break
			}
		}
		waitErr := cmd.Wait()
		m.mu.Lock()
		m.connecting = false
		m.connectDone = waitErr == nil
		if waitErr != nil {
			m.connectError = waitErr.Error()
			m.phase = dingtalkPhaseError
			m.statusMessage = "钉钉授权失败，请重新开始连接"
		} else {
			m.phase = dingtalkPhaseConnected
			m.statusMessage = "钉钉已授权，请选择群聊和机器人"
		}
		m.connectCmd = nil
		m.mu.Unlock()
	}()
	response.Success(c, gin.H{"message": "DingTalk authorization started"})
}

func (m *dingtalkManager) parseAuthOutput(output string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "login.dingtalk.com/oauth2/device/verify.htm") {
			start := strings.Index(line, "https://")
			if start >= 0 {
				url := strings.TrimRight(line[start:], " \t\r\n\"')>")
				m.mu.Lock()
				m.verificationURL = url
				m.phase = dingtalkPhaseAwaitingAuthorization
				m.statusMessage = "请扫描二维码完成钉钉授权"
				m.mu.Unlock()
			}
		}
		if strings.Contains(line, "authorization code:") {
			parts := strings.SplitN(line, "authorization code:", 2)
			if len(parts) == 2 {
				m.mu.Lock()
				m.authorizationCode = strings.TrimSpace(parts[1])
				m.mu.Unlock()
			}
		}
	}
}

func dingtalkConnectStatus(c *gin.Context) {
	m := globalDingTalk
	m.mu.Lock()
	defer m.mu.Unlock()
	response.Success(c, gin.H{
		"connecting":         m.connecting,
		"done":               m.connectDone,
		"phase":              m.phase,
		"status_message":     m.statusMessage,
		"verification_url":   m.verificationURL,
		"authorization_code": m.authorizationCode,
		"error":              m.connectError,
	})
}

func dingtalkConnectCancel(c *gin.Context) {
	m := globalDingTalk
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectCmd != nil && m.connectCmd.Process != nil {
		_ = m.connectCmd.Process.Kill()
	}
	m.connectCmd = nil
	m.connecting = false
	m.connectDone = false
	m.connectError = ""
	m.verificationURL = ""
	m.authorizationCode = ""
	m.phase = dingtalkPhaseIdle
	m.statusMessage = "连接已取消"
	response.Success(c, gin.H{"cancelled": true})
}

func dingtalkStatus(c *gin.Context) {
	m := globalDingTalk
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "auth", "status", "--format", "json")
	authenticated := false
	if err == nil {
		var payload map[string]any
		if json.Unmarshal([]byte(out), &payload) == nil {
			if value, ok := payload["authenticated"].(bool); ok {
				authenticated = value
			}
		}
	}
	m.loadTarget()
	m.mu.Lock()
	groupID, robotCode := m.groupID, m.robotCode
	if authenticated {
		if groupID != "" && robotCode != "" {
			m.phase = dingtalkPhaseReady
			m.statusMessage = "已连接并配置告警群，可以发送测试消息"
		} else if !m.connecting {
			m.phase = dingtalkPhaseConnected
			m.statusMessage = "钉钉已授权，请选择群聊和机器人"
		}
	} else if !m.connecting {
		m.phase = dingtalkPhaseIdle
		m.statusMessage = "尚未完成钉钉授权"
	}
	phase, statusMessage := m.phase, m.statusMessage
	m.mu.Unlock()
	response.Success(c, gin.H{
		"connected":             authenticated,
		"phase":                 phase,
		"status_message":        statusMessage,
		"target_configured":     groupID != "" && robotCode != "",
		"group_id":              groupID,
		"robot_code":            robotCode,
		"robot_name":            m.robotName,
		"app_name":              m.appName,
		"unified_app_id":        m.appID,
		"create_phase":          m.createPhase,
		"create_task_id":        m.createTaskID,
		"create_error":          m.createError,
		"create_status_message": m.createStatusMessage,
		"error":                 errorString(err),
	})
}

func dingtalkGroups(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		response.BadRequest(c, "query is required to search DingTalk groups")
		return
	}
	m := globalDingTalk
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "chat", "search", "--query", query, "--format", "json")
	if err != nil {
		response.InternalError(c, fmt.Sprintf("search DingTalk groups failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	response.Success(c, gin.H{"groups": normalizeDingTalkItems(out)})
}

func dingtalkRobots(c *gin.Context) {
	m := globalDingTalk
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "chat", "bot", "search", "--page", "1", "--size", "50", "--format", "json")
	if err != nil {
		response.InternalError(c, fmt.Sprintf("list DingTalk bots failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	response.Success(c, gin.H{"robots": normalizeDingTalkItems(out)})
}

type dingtalkCreateRobotRequest struct {
	AppName     string `json:"app_name"`
	RobotName   string `json:"robot_name"`
	Description string `json:"description"`
}

func dingtalkCreateRobot(c *gin.Context) {
	var req dingtalkCreateRobotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req.AppName = strings.TrimSpace(req.AppName)
	req.RobotName = strings.TrimSpace(req.RobotName)
	req.Description = strings.TrimSpace(req.Description)
	if req.AppName == "" || req.RobotName == "" || req.Description == "" {
		response.BadRequest(c, "app_name, robot_name and description are required")
		return
	}
	if len([]rune(req.AppName)) < 2 || len([]rune(req.AppName)) > 20 {
		response.BadRequest(c, "app_name must contain 2-20 characters")
		return
	}
	if len([]rune(req.Description)) > 200 {
		response.BadRequest(c, "description must contain at most 200 characters")
		return
	}

	m := globalDingTalk
	m.mu.Lock()
	if m.createPhase == dingtalkCreatePhaseSubmitting || m.createPhase == dingtalkCreatePhaseAwaitingResult {
		m.mu.Unlock()
		response.BadRequest(c, "DingTalk robot creation is already in progress")
		return
	}
	m.createPhase = dingtalkCreatePhaseSubmitting
	m.createTaskID = ""
	m.createError = ""
	m.createStatusMessage = "正在提交钉钉机器人创建任务..."
	m.appName = req.AppName
	m.robotName = req.RobotName
	m.mu.Unlock()

	go m.createRobot(req)
	response.Success(c, gin.H{
		"phase":          dingtalkCreatePhaseSubmitting,
		"status_message": "正在提交钉钉机器人创建任务...",
	})
}

func (m *dingtalkManager) createRobot(req dingtalkCreateRobotRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	out, err := m.runCLI(ctx, "dev", "app", "robot", "submit",
		"--name", req.AppName,
		"--robot-name", req.RobotName,
		"--desc", req.Description,
		"--yes", "--format", "json")
	if err != nil {
		m.setDingTalkCreateError(fmt.Sprintf("提交机器人创建任务失败: %s", strings.TrimSpace(out)))
		return
	}
	taskID := findDingTalkString(out, "taskId", "task_id")
	if taskID == "" {
		m.setDingTalkCreateError("钉钉 CLI 未返回创建任务 ID")
		return
	}
	m.mu.Lock()
	m.createPhase = dingtalkCreatePhaseAwaitingResult
	m.createTaskID = taskID
	m.createStatusMessage = "机器人创建任务已提交，正在等待钉钉返回结果..."
	m.mu.Unlock()

	for attempt := 0; attempt < 90; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				m.setDingTalkCreateError("等待钉钉机器人创建结果超时")
				return
			case <-time.After(2 * time.Second):
			}
		}
		resultCtx, resultCancel := context.WithTimeout(ctx, 30*time.Second)
		resultOut, resultErr := m.runCLI(resultCtx, "dev", "app", "robot", "result", "--task-id", taskID, "--format", "json")
		resultCancel()
		if resultErr != nil {
			m.setDingTalkCreateError(fmt.Sprintf("查询机器人创建结果失败: %s", strings.TrimSpace(resultOut)))
			return
		}
		robotCode := findDingTalkString(resultOut, "robotCode", "robot_code")
		appID := findDingTalkString(resultOut, "unifiedAppId", "unified_app_id", "appId", "app_id")
		status := strings.ToLower(findDingTalkString(resultOut, "status", "state", "resultStatus", "result_status"))
		if isDingTalkCreateFailure(status) {
			m.setDingTalkCreateError("钉钉机器人创建失败: " + status)
			return
		}
		if robotCode != "" {
			m.finishDingTalkRobot(appID, robotCode)
			return
		}
	}
	m.setDingTalkCreateError("等待钉钉机器人创建结果超时")
}

func (m *dingtalkManager) finishDingTalkRobot(appID, robotCode string) {
	m.mu.Lock()
	groupID := m.groupID
	m.appID = strings.TrimSpace(appID)
	m.robotCode = strings.TrimSpace(robotCode)
	m.createPhase = dingtalkCreatePhaseCreated
	m.createError = ""
	m.createStatusMessage = "钉钉机器人已创建，请选择群聊并绑定"
	m.mu.Unlock()
	if err := m.saveTarget(groupID, robotCode); err != nil {
		m.setDingTalkCreateError("机器人已创建，但保存配置失败: " + err.Error())
	}
}

func (m *dingtalkManager) setDingTalkCreateError(message string) {
	m.mu.Lock()
	m.createPhase = dingtalkCreatePhaseError
	m.createError = message
	m.createStatusMessage = "机器人创建失败，请检查授权和 dws CLI 后重试"
	m.mu.Unlock()
}

func dingtalkCreateRobotStatus(c *gin.Context) {
	m := globalDingTalk
	m.mu.Lock()
	defer m.mu.Unlock()
	response.Success(c, gin.H{
		"phase":          m.createPhase,
		"task_id":        m.createTaskID,
		"robot_code":     m.robotCode,
		"robot_name":     m.robotName,
		"unified_app_id": m.appID,
		"app_name":       m.appName,
		"error":          m.createError,
		"status_message": m.createStatusMessage,
	})
}

func dingtalkBindTarget(c *gin.Context) {
	var req struct {
		GroupID   string `json:"group_id"`
		RobotCode string `json:"robot_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req.GroupID, req.RobotCode = strings.TrimSpace(req.GroupID), strings.TrimSpace(req.RobotCode)
	if req.GroupID == "" || req.RobotCode == "" {
		response.BadRequest(c, "group_id and robot_code are required")
		return
	}
	m := globalDingTalk
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "chat", "group", "members", "add-bot", "--id", req.GroupID, "--robot-code", req.RobotCode, "--yes", "--format", "json")
	if err != nil {
		response.InternalError(c, fmt.Sprintf("add DingTalk bot to group failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	if err := m.saveTarget(req.GroupID, req.RobotCode); err != nil {
		response.InternalError(c, "save DingTalk target failed: "+err.Error())
		return
	}
	m.mu.Lock()
	m.groupID, m.robotCode = req.GroupID, req.RobotCode
	m.phase = dingtalkPhaseReady
	m.statusMessage = "机器人已加入告警群，可以发送测试消息"
	m.mu.Unlock()
	response.Success(c, gin.H{"group_id": req.GroupID, "robot_code": req.RobotCode, "output": strings.TrimSpace(out)})
}

func dingtalkSetTarget(c *gin.Context) {
	var req struct {
		GroupID   string `json:"group_id"`
		RobotCode string `json:"robot_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	req.GroupID, req.RobotCode = strings.TrimSpace(req.GroupID), strings.TrimSpace(req.RobotCode)
	if req.GroupID == "" || req.RobotCode == "" {
		response.BadRequest(c, "group_id and robot_code are required")
		return
	}
	m := globalDingTalk
	if err := m.saveTarget(req.GroupID, req.RobotCode); err != nil {
		response.InternalError(c, "save DingTalk target failed: "+err.Error())
		return
	}
	m.mu.Lock()
	m.groupID, m.robotCode = req.GroupID, req.RobotCode
	m.phase = dingtalkPhaseReady
	m.statusMessage = "告警群和机器人已保存，可以发送测试消息"
	m.mu.Unlock()
	response.Success(c, gin.H{"group_id": req.GroupID, "robot_code": req.RobotCode})
}

func dingtalkSendTest(c *gin.Context) {
	var req struct {
		Markdown string `json:"markdown"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Markdown) == "" {
		req.Markdown = "**Sub2API 钉钉通知测试**\n\n如果你看到这条消息，说明通知通道已连通。"
	}
	doDingTalkSend(c, req.Markdown)
}

func doDingTalkSend(c *gin.Context, markdown string) {
	m := globalDingTalk
	m.loadTarget()
	m.mu.Lock()
	groupID, robotCode := m.groupID, m.robotCode
	m.mu.Unlock()
	if groupID == "" || robotCode == "" {
		response.BadRequest(c, "no DingTalk target configured")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	out, err := m.runCLI(ctx, "chat", "message", "send-by-bot", "--robot-code", robotCode, "--group", groupID, "--title", "Sub2API 通知", "--text", markdown, "--format", "json")
	if err != nil {
		response.InternalError(c, fmt.Sprintf("send DingTalk message failed: %s: %s", err, strings.TrimSpace(out)))
		return
	}
	response.Success(c, gin.H{"sent": true, "output": strings.TrimSpace(out)})
}

func umDispatchNotifications(ctx context.Context, alerts []upstreamAlert) {
	switch currentNotificationProvider() {
	case notificationProviderDingTalk:
		umDispatchDingTalk(ctx, alerts)
	case notificationProviderWeChat:
		umDispatchWeChat(ctx, alerts)
	case notificationProviderClaw163:
		umDispatchClaw163(ctx, alerts)
	default:
		umDispatchFeishu(ctx, alerts)
	}
}

func umDispatchClaw163(ctx context.Context, alerts []upstreamAlert) {
	var body strings.Builder
	for i, alert := range alerts {
		if i > 0 {
			body.WriteString("\n---\n")
		}
		body.WriteString(fmt.Sprintf("[%s] %s\n", alert.TargetName, alert.Message))
		if alert.Detail != "" {
			body.WriteString(alert.Detail + "\n")
		}
		body.WriteString(alert.TriggeredAt.Format("2006-01-02 15:04:05"))
	}
	_ = pluginruntime.SendClaw163Notification(ctx, "Sub2API 上游告警", body.String())
}

func umDispatchDingTalk(ctx context.Context, alerts []upstreamAlert) {
	var sb strings.Builder
	for i, alert := range alerts {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(fmt.Sprintf("**[%s] %s**\n", alert.TargetName, alert.Message))
		if alert.Detail != "" {
			sb.WriteString(alert.Detail + "\n")
		}
		sb.WriteString(alert.TriggeredAt.Format("2006-01-02 15:04:05"))
	}
	m := globalDingTalk
	m.loadTarget()
	m.mu.Lock()
	groupID, robotCode := m.groupID, m.robotCode
	m.mu.Unlock()
	if groupID == "" || robotCode == "" {
		return
	}
	_, _ = m.runCLI(ctx, "chat", "message", "send-by-bot", "--robot-code", robotCode, "--group", groupID, "--title", "Sub2API 上游告警", "--text", sb.String(), "--format", "json")
}

func umDispatchWeChat(ctx context.Context, alerts []upstreamAlert) {
	var sb strings.Builder
	for i, alert := range alerts {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", alert.TargetName, alert.Message))
		if alert.Detail != "" {
			sb.WriteString(alert.Detail + "\n")
		}
		sb.WriteString(alert.TriggeredAt.Format("2006-01-02 15:04:05"))
	}
	_, _ = globalWeChat.sendText(ctx, sb.String())
}

func normalizeDingTalkItems(raw string) []gin.H {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return []gin.H{}
	}
	value = unwrapDingTalkValue(value)
	items := make([]gin.H, 0)
	collectDingTalkItems(value, &items)
	return items
}

func unwrapDingTalkValue(value any) any {
	for {
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		advanced := false
		for _, key := range []string{"result", "body", "data", "items", "groups", "robots", "robotList", "robot_list", "bots", "botList"} {
			if next, exists := object[key]; exists {
				if key == "items" || key == "groups" || key == "robots" || key == "robotList" || key == "robot_list" || key == "bots" || key == "botList" {
					return next
				}
				value = next
				advanced = true
				break
			}
		}
		if !advanced {
			return value
		}
	}
}

func collectDingTalkItems(value any, items *[]gin.H) {
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			collectDingTalkItems(item, items)
		}
	case map[string]any:
		id := firstDingTalkString(current, "openConversationId", "openconversation_id", "conversationId", "groupId", "id", "robotCode", "robot_code")
		name := firstDingTalkString(current, "name", "groupName", "robotName", "title")
		code := firstDingTalkString(current, "robotCode", "robot_code", "code")
		if id != "" || code != "" {
			item := gin.H{"id": id, "name": name}
			if code != "" {
				item["robot_code"] = code
			}
			*items = append(*items, item)
			return
		}
		for _, key := range []string{"result", "body", "data", "items", "groups", "robots", "robotList", "robot_list", "bots", "botList"} {
			if next, exists := current[key]; exists {
				collectDingTalkItems(next, items)
			}
		}
	}
}

func firstDingTalkString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func findDingTalkString(raw string, keys ...string) string {
	value, ok := parseDingTalkJSON(raw)
	if !ok {
		return ""
	}
	return findDingTalkStringValue(value, keys...)
}

func parseDingTalkJSON(raw string) (any, bool) {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return value, true
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start || json.Unmarshal([]byte(raw[start:end+1]), &value) != nil {
		return nil, false
	}
	return value, true
}

func findDingTalkStringValue(value any, keys ...string) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if candidate, ok := current[key].(string); ok && strings.TrimSpace(candidate) != "" {
				return strings.TrimSpace(candidate)
			}
		}
		for _, nested := range current {
			if candidate := findDingTalkStringValue(nested, keys...); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range current {
			if candidate := findDingTalkStringValue(nested, keys...); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func isDingTalkCreateFailure(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "failure", "error", "rejected", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
