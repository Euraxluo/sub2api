// Package claw163 owns the Claw163 CLI notification channel.
//
// The host only knows the provider facade. Credentials, profile files, and
// all mail transport operations stay behind the plugin-owned CLI adapter.
package claw163

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	mailaddr "net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	ProviderID = "claw163"

	// DefaultAuthURL is the short-lived bootstrap token supplied for the local
	// Admin Tools setup. CLAW163_AUTH_URL can replace it per deployment.
	DefaultAuthURL = "t1/qzFVTmZuR8c77xz4Cq86yFNaQew"

	defaultProfileDir = "/app/data/claw163-profile"
	maxAuthURLLength  = 2048
	maxAuthBodyBytes  = 128 << 10
	maxRecipients     = 20
	maxBodyBytes      = 512 << 10
	maxSubjectLength  = 200
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+\-]{0,63}$`)

// Account is safe mailbox metadata returned to Admin Tools. Credentials are
// never persisted in this structure.
type Account struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Transport string `json:"transport"`
}

type Status struct {
	Provider      string    `json:"provider"`
	CLIInstalled  bool      `json:"cli_installed"`
	CLIPath       string    `json:"cli_path,omitempty"`
	CLIVersion    string    `json:"cli_version,omitempty"`
	Initialized   bool      `json:"initialized"`
	Ready         bool      `json:"ready"`
	Phase         string    `json:"phase"`
	StatusMessage string    `json:"status_message"`
	Profile       string    `json:"profile,omitempty"`
	Sender        string    `json:"sender,omitempty"`
	Accounts      []Account `json:"accounts"`
	Recipients    []string  `json:"recipients"`
	SetupAuthURL  string    `json:"setup_auth_url,omitempty"`
	SetupCommand  string    `json:"setup_command,omitempty"`
	LastError     string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type InitializeRequest struct {
	AuthURL    string   `json:"auth_url"`
	Recipients []string `json:"recipients"`
}

type TargetRequest struct {
	Recipients []string `json:"recipients"`
}

type SendRequest struct {
	Recipients []string `json:"recipients"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
}

type storedState struct {
	InitializedAt time.Time `json:"initialized_at"`
	Profile       string    `json:"profile"`
	Sender        string    `json:"sender"`
	Accounts      []Account `json:"accounts"`
	Recipients    []string  `json:"recipients"`
}

type authAccount struct {
	Account
	Credential string
}

// commandRunner is injectable for unit tests. A nil runner executes a process.
type commandRunner func(context.Context, string, []string, []string) ([]byte, error)

type Manager struct {
	mu          sync.Mutex
	operationMu sync.Mutex

	profileDir string
	configPath string
	xdgConfig  string
	cliPath    string
	runner     commandRunner
	httpClient *http.Client
	now        func() time.Time
}

var globalManager = NewManager("")

// Install is intentionally side-effect free. The CLI is installed in the
// image; no network or filesystem work happens during server startup.
func Install() {}

func NewManager(profileDir string) *Manager {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir == "" {
		profileDir = strings.TrimSpace(os.Getenv("CLAW163_PROFILE_DIR"))
	}
	if profileDir == "" {
		profileDir = defaultProfileDir
	}
	return &Manager{
		profileDir: profileDir,
		configPath: filepath.Join(profileDir, "mail-cli.json"),
		xdgConfig:  filepath.Join(profileDir, "xdg-config"),
		httpClient: &http.Client{Timeout: 20 * time.Second},
		now:        time.Now,
	}
}

func DefaultSetupAuthURL() string {
	if value := strings.TrimSpace(os.Getenv("CLAW163_AUTH_URL")); value != "" {
		return value
	}
	return DefaultAuthURL
}

// SetupCommand is shown in Admin Tools so the Claw163 bootstrap is explicit.
// Notification delivery uses the official mail-cli installed in the image.
func SetupCommand(rawAuthURL string) string {
	authURL := strings.TrimSpace(rawAuthURL)
	if authURL == "" {
		authURL = DefaultSetupAuthURL()
	}
	authURL = strings.ReplaceAll(authURL, `"`, `\"`)
	return fmt.Sprintf(`npx "@clawemail/claw-setup@latest" --auth-url "%s"`, authURL)
}

func (m *Manager) Status(ctx context.Context) Status {
	state, stateErr := m.loadState()
	status := Status{
		Provider:      ProviderID,
		Phase:         "idle",
		StatusMessage: "尚未初始化 Claw163 邮箱",
		Accounts:      append([]Account(nil), state.Accounts...),
		Recipients:    append([]string(nil), state.Recipients...),
		SetupAuthURL:  DefaultSetupAuthURL(),
		SetupCommand:  SetupCommand(DefaultSetupAuthURL()),
		UpdatedAt:     state.InitializedAt,
	}
	if stateErr != nil {
		status.LastError = "读取 Claw163 配置失败"
	}

	cli, err := m.resolveCLI()
	if err != nil {
		status.LastError = err.Error()
		status.StatusMessage = "未安装 Claw163 mail-cli，请重建 Sub2API 镜像"
		return status
	}
	status.CLIInstalled = true
	status.CLIPath = cli
	status.CLIVersion = m.version(ctx)
	status.Profile = state.Profile
	status.Sender = state.Sender
	status.Initialized = state.Profile != ""
	if !status.Initialized {
		return status
	}

	status.SetupAuthURL = ""
	status.SetupCommand = ""
	checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if _, err := m.runCLI(checkCtx, state.Profile, "auth", "test"); err != nil {
		status.Phase = "error"
		status.LastError = "Claw163 mail-cli 账号校验失败"
		status.StatusMessage = "邮箱凭据不可用，请重新初始化"
		return status
	}
	if len(state.Recipients) == 0 {
		status.Phase = "connected"
		status.StatusMessage = "邮箱已初始化，请配置通知收件人"
		return status
	}
	status.Ready = true
	status.Phase = "ready"
	status.StatusMessage = "Claw163 邮箱已就绪，可以发送通知"
	return status
}

// Initialize consumes the one-time auth URL and performs all account changes
// through the official mail-cli. The URL and credentials are never written to state.
func (m *Manager) Initialize(ctx context.Context, rawAuthURL string, recipients []string) (Status, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()

	if _, err := m.resolveCLI(); err != nil {
		return Status{}, err
	}
	authURL, err := normalizeAuthURL(rawAuthURL)
	if err != nil {
		return Status{}, err
	}
	oldState, _ := m.loadState()
	targets := recipients
	if len(targets) == 0 {
		targets = oldState.Recipients
	}
	targets, err = normalizeRecipients(targets)
	if err != nil {
		return Status{}, err
	}

	body, err := m.fetchAuthURL(ctx, authURL)
	if err != nil {
		return Status{}, err
	}
	apiKey, accounts, err := parseAuthPayload(body)
	if err != nil {
		return Status{}, err
	}
	if err := m.ensureDirs(); err != nil {
		return Status{}, err
	}

	if apiKey != "" {
		if _, err := m.runCLI(ctx, "default", "auth", "apikey", "set", apiKey); err != nil {
			return Status{}, errors.New("保存 Claw163 API Key 失败")
		}
	}
	storedAccounts := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if _, err := m.loginAccount(ctx, account); err != nil {
			return Status{}, err
		}
		storedAccounts = append(storedAccounts, account.Account)
	}

	selected := accounts[0]
	for _, account := range accounts {
		if account.ID == "default" {
			selected = account
			break
		}
	}
	state := storedState{
		InitializedAt: m.now().UTC(),
		Profile:       selected.ID,
		Sender:        selected.Email,
		Accounts:      storedAccounts,
		Recipients:    targets,
	}
	if err := m.saveState(state); err != nil {
		return Status{}, err
	}
	return m.Status(ctx), nil
}

func (m *Manager) SetRecipients(recipients []string) (Status, error) {
	targets, err := normalizeRecipients(recipients)
	if err != nil {
		return Status{}, err
	}
	state, err := m.loadState()
	if err != nil || state.Profile == "" {
		return Status{}, errors.New("请先初始化 Claw163 邮箱")
	}
	state.Recipients = targets
	if err := m.saveState(state); err != nil {
		return Status{}, err
	}
	return m.Status(context.Background()), nil
}

func (m *Manager) Send(ctx context.Context, subject, body string, recipients []string) error {
	state, err := m.loadState()
	if err != nil || state.Profile == "" {
		return errors.New("Claw163 邮箱尚未初始化")
	}
	if len(recipients) == 0 {
		recipients = state.Recipients
	}
	recipients, err = normalizeRecipients(recipients)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return errors.New("Claw163 通知收件人未配置")
	}
	subject, err = normalizeSubject(subject)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("通知正文不能为空")
	}
	if len([]byte(body)) > maxBodyBytes {
		return errors.New("通知正文过大")
	}
	_, err = m.runCLI(ctx, state.Profile,
		"compose", "send",
		"--to", strings.Join(recipients, ","),
		"--subject", subject,
		"--body", body,
	)
	if err != nil {
		return fmt.Errorf("Claw163 mail-cli 发送邮件失败: %w", err)
	}
	return nil
}

func GlobalStatus(ctx context.Context) Status { return globalManager.Status(ctx) }

func Initialize(ctx context.Context, authURL string, recipients []string) (Status, error) {
	return globalManager.Initialize(ctx, authURL, recipients)
}

func SetRecipients(recipients []string) (Status, error) {
	return globalManager.SetRecipients(recipients)
}

func Send(ctx context.Context, subject, body string) error {
	return globalManager.Send(ctx, subject, body, nil)
}

func (m *Manager) loginAccount(ctx context.Context, account authAccount) ([]byte, error) {
	args := []string{"auth", "login", "--user", account.Email}
	if account.Transport == "imap" {
		args = append(args,
			"--auth-method", "password",
			"--password", account.Credential,
			"--imap-host", "imap.claw.163.com",
			"--imap-port", "993",
			"--smtp-host", "smtp.claw.163.com",
			"--smtp-port", "465",
		)
	}
	output, err := m.runCLI(ctx, account.ID, args...)
	if err != nil {
		return output, fmt.Errorf("注册 Claw163 账号 %s 失败", account.ID)
	}
	return output, nil
}

func (m *Manager) runCLI(ctx context.Context, profile string, args ...string) ([]byte, error) {
	if err := m.ensureDirs(); err != nil {
		return nil, err
	}
	cli, err := m.resolveCLI()
	if err != nil {
		return nil, err
	}
	commandArgs := []string{"--config", m.configPath}
	if profile != "" && profile != "default" {
		commandArgs = append(commandArgs, "--profile", profile)
	}
	commandArgs = append(commandArgs, "--json")
	commandArgs = append(commandArgs, args...)
	env := m.cliEnv()
	var output []byte
	if m.runner != nil {
		output, err = m.runner(ctx, cli, commandArgs, env)
	} else {
		output, err = runCommand(ctx, cli, commandArgs, env)
	}
	if message := cliErrorMessage(output); message != "" {
		return output, errors.New(message)
	}
	if err != nil {
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return output, fmt.Errorf("%w: %s", err, detail)
		}
		return output, err
	}
	return output, nil
}

func (m *Manager) version(ctx context.Context) string {
	cli, err := m.resolveCLI()
	if err != nil {
		return ""
	}
	env := m.cliEnv()
	var output []byte
	if m.runner != nil {
		output, err = m.runner(ctx, cli, []string{"--version"}, env)
	} else {
		output, err = runCommand(ctx, cli, []string{"--version"}, env)
	}
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func (m *Manager) resolveCLI() (string, error) {
	if m.runner != nil {
		return "mail-cli", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cliPath != "" {
		if _, err := os.Stat(m.cliPath); err == nil {
			return m.cliPath, nil
		}
		m.cliPath = ""
	}
	for _, candidate := range []string{os.Getenv("CLAW163_MAIL_CLI_PATH"), os.Getenv("CLAW163_CLI_PATH"), "mail-cli", "/usr/local/bin/mail-cli", "/app/data/bin/mail-cli"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if filepath.Base(candidate) == candidate {
			path, err := exec.LookPath(candidate)
			if err != nil {
				continue
			}
			m.cliPath = path
			return path, nil
		}
		if _, err := os.Stat(candidate); err == nil {
			m.cliPath = candidate
			return candidate, nil
		}
	}
	return "", errors.New("Claw163 mail-cli 未安装，请重建 Sub2API 镜像")
}

func (m *Manager) ensureDirs() error {
	if err := os.MkdirAll(m.profileDir, 0o700); err != nil {
		return err
	}
	return os.MkdirAll(m.xdgConfig, 0o700)
}

func (m *Manager) cliEnv() []string {
	env := os.Environ()
	setEnv := func(key, value string) {
		prefix := key + "="
		for i, item := range env {
			if strings.HasPrefix(item, prefix) {
				env[i] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}
	setEnv("HOME", m.profileDir)
	setEnv("XDG_CONFIG_HOME", m.xdgConfig)
	return env
}

func (m *Manager) statePath() string { return filepath.Join(m.profileDir, "notification-state.json") }

func (m *Manager) loadState() (storedState, error) {
	data, err := os.ReadFile(m.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return storedState{}, nil
	}
	if err != nil {
		return storedState{}, err
	}
	var state storedState
	if err := json.Unmarshal(data, &state); err != nil {
		return storedState{}, err
	}
	return state, nil
}

func (m *Manager) saveState(state storedState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := m.ensureDirs(); err != nil {
		return err
	}
	path := m.statePath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (m *Manager) fetchAuthURL(ctx context.Context, authURL string) (string, error) {
	client := m.httpClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("授权链接重定向次数过多")
		}
		if request.URL.Scheme != "https" || !isAllowedAuthHost(request.URL.Hostname()) {
			return errors.New("授权链接重定向到不受信任的地址")
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return "", errors.New("授权链接无效")
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "sub2api-claw163/1")
	response, err := clientCopy.Do(request)
	if err != nil {
		return "", errors.New("获取 Claw163 授权信息失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("授权链接返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAuthBodyBytes+1))
	if err != nil || len(body) > maxAuthBodyBytes {
		return "", errors.New("授权信息读取失败或内容过大")
	}
	text := strings.TrimSpace(string(body))
	if text == "" || strings.Contains(strings.ToLower(text), "<html") {
		return "", errors.New("授权链接无效或已过期")
	}
	return text, nil
}

func normalizeAuthURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultSetupAuthURL()
	}
	if len(raw) > maxAuthURLLength {
		return "", errors.New("授权链接过长")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		raw = "https://u.163.com/" + strings.TrimPrefix(raw, "/")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme != "https" || !isAllowedAuthHost(parsed.Hostname()) || !strings.HasPrefix(parsed.Path, "/t1/") {
		return "", errors.New("授权链接必须是 https://u.163.com/t1/... 格式")
	}
	return parsed.String(), nil
}

func isAllowedAuthHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "u.163.com" || strings.HasSuffix(host, ".163.com")
}

func parseAuthPayload(body string) (string, []authAccount, error) {
	var apiKey string
	accounts := make([]authAccount, 0)
	seen := make(map[string]struct{})
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		first := strings.IndexByte(line, ':')
		if first <= 0 {
			return "", nil, errors.New("授权信息格式无效")
		}
		secondRelative := strings.IndexByte(line[first+1:], ':')
		if secondRelative < 0 {
			return "", nil, errors.New("授权信息格式无效")
		}
		second := first + 1 + secondRelative
		name := strings.TrimSpace(line[:first])
		accountID := strings.TrimSpace(line[first+1 : second])
		credential := strings.TrimSpace(line[second+1:])
		if name == "__apikey__" {
			if apiKey != "" || credential == "" {
				return "", nil, errors.New("授权信息缺少有效 API Key")
			}
			apiKey = credential
			continue
		}
		if !identifierPattern.MatchString(name) || !identifierPattern.MatchString(accountID) {
			return "", nil, errors.New("授权信息包含无效账号标识")
		}
		if _, ok := seen[accountID]; ok {
			return "", nil, errors.New("授权信息包含重复账号")
		}
		seen[accountID] = struct{}{}
		transport := "ws"
		if credential != "" {
			transport = "imap"
		}
		accounts = append(accounts, authAccount{
			Account:    Account{ID: accountID, Name: name, Email: name + "@claw.163.com", Transport: transport},
			Credential: credential,
		})
	}
	if len(accounts) == 0 {
		return "", nil, errors.New("授权信息没有邮箱账号")
	}
	for _, account := range accounts {
		if account.Transport == "ws" && apiKey == "" {
			return "", nil, errors.New("WebSocket 账号缺少 API Key")
		}
	}
	return apiKey, accounts, nil
}

func normalizeRecipients(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
			value := strings.TrimSpace(part)
			if value == "" {
				continue
			}
			parsed, err := mailaddr.ParseAddress(value)
			if err != nil || parsed.Address != value {
				return nil, fmt.Errorf("通知收件人地址无效: %s", value)
			}
			if len(value) > 254 {
				return nil, errors.New("通知收件人地址过长")
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
			if len(result) > maxRecipients {
				return nil, errors.New("通知收件人数量过多")
			}
		}
	}
	return result, nil
}

func normalizeSubject(subject string) (string, error) {
	subject = strings.Join(strings.Fields(strings.TrimSpace(subject)), " ")
	if subject == "" {
		subject = "Sub2API 通知"
	}
	if len([]rune(subject)) > maxSubjectLength {
		return "", errors.New("通知主题过长")
	}
	return subject, nil
}

func runCommand(ctx context.Context, name string, args, env []string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = env
	return command.CombinedOutput()
}

func cliErrorMessage(output []byte) string {
	var payload struct {
		Success *bool `json:"success"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(output, &payload) != nil || payload.Success == nil || *payload.Success {
		return ""
	}
	if message := strings.TrimSpace(payload.Error.Message); message != "" {
		return message
	}
	return "Claw163 mail-cli 返回失败"
}
