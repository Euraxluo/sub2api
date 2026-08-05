package billing_strategy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	accountSnapshotPathPrefix        = "/api/v1/admin/accounts/"
	accountSnapshotDefaultPort       = "8080"
	accountSnapshotMaxResponseBytes  = 1 << 20
	accountSnapshotRequestTimeout    = 5 * time.Second
	accountSnapshotBaseURLEnv        = "SUB2API_BILLING_STRATEGY_BASE_URL"
	accountSnapshotAdminAPIKeyEnv    = "SUB2API_BILLING_STRATEGY_ADMIN_API_KEY"
	accountSnapshotAdminAPIKeyHeader = "x-api-key"
)

var (
	ErrAccountSnapshotAuthMissing = errors.New("billing strategy account snapshot authentication is not configured")
	ErrAccountSnapshotInvalid     = errors.New("billing strategy account snapshot is invalid")
)

// AccountSnapshot is the account pricing state used by one billing decision.
// The snapshot is fetched once and then reused for the virtual price, audit,
// and downstream account-cost accounting.
type AccountSnapshot struct {
	AccountID      int64
	RateMultiplier float64
}

type accountSnapshotEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ID             int64    `json:"id"`
		RateMultiplier *float64 `json:"rate_multiplier"`
	} `json:"data"`
}

type accountSnapshotHTTPConfig struct {
	baseURL       string
	adminAPIKey   string
	authorization string
}

var accountSnapshotHTTPState struct {
	sync.RWMutex
	config accountSnapshotHTTPConfig
}

var accountSnapshotHTTPClient = &http.Client{
	Timeout: accountSnapshotRequestTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// rememberAccountSnapshotAdminRequest captures the already-authenticated
// billing-strategy admin request. Session-bound JWTs are deliberately ignored:
// replaying one from a loopback request would violate its IP/UA binding. Plain
// admin JWTs and x-api-key credentials remain in memory only; the environment
// variable is the unattended-startup source.
func rememberAccountSnapshotAdminRequest(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	baseURL := accountSnapshotBaseURLFromRequest(c.Request)
	adminAPIKey := strings.TrimSpace(c.Request.Header.Get(accountSnapshotAdminAPIKeyHeader))
	authorization := reusableAdminAuthorization(c.Request)
	if baseURL == "" && adminAPIKey == "" && authorization == "" {
		return
	}

	accountSnapshotHTTPState.Lock()
	defer accountSnapshotHTTPState.Unlock()
	if baseURL != "" {
		accountSnapshotHTTPState.config.baseURL = baseURL
	}
	if adminAPIKey != "" {
		accountSnapshotHTTPState.config.adminAPIKey = adminAPIKey
	}
	if authorization != "" {
		accountSnapshotHTTPState.config.authorization = authorization
	}
}

func fetchAccountSnapshot(ctx context.Context, accountID int64) (AccountSnapshot, error) {
	if accountID <= 0 {
		return AccountSnapshot{}, fmt.Errorf("%w: account id must be positive", ErrAccountSnapshotInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL, headerName, headerValue, err := resolveAccountSnapshotHTTPConfig()
	if err != nil {
		return AccountSnapshot{}, err
	}
	target := strings.TrimRight(baseURL, "/") + accountSnapshotPathPrefix + strconv.FormatInt(accountID, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("create account snapshot request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set(headerName, headerValue)

	response, err := accountSnapshotHTTPClient.Do(request)
	if err != nil {
		return AccountSnapshot{}, fmt.Errorf("fetch account %d snapshot: %w", accountID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return AccountSnapshot{}, fmt.Errorf("fetch account %d snapshot: HTTP %d", accountID, response.StatusCode)
	}

	var envelope accountSnapshotEnvelope
	decoder := json.NewDecoder(io.LimitReader(response.Body, accountSnapshotMaxResponseBytes))
	if err := decoder.Decode(&envelope); err != nil {
		return AccountSnapshot{}, fmt.Errorf("decode account %d snapshot: %w", accountID, err)
	}
	if envelope.Code != 0 {
		return AccountSnapshot{}, fmt.Errorf("fetch account %d snapshot: code %d: %s", accountID, envelope.Code, strings.TrimSpace(envelope.Message))
	}
	if envelope.Data.ID != accountID {
		return AccountSnapshot{}, fmt.Errorf("%w: requested account %d, received account %d", ErrAccountSnapshotInvalid, accountID, envelope.Data.ID)
	}
	if envelope.Data.RateMultiplier == nil {
		return AccountSnapshot{}, fmt.Errorf("%w: account %d rate_multiplier is missing", ErrAccountSnapshotInvalid, accountID)
	}
	rate := *envelope.Data.RateMultiplier
	if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return AccountSnapshot{}, fmt.Errorf("%w: account %d rate_multiplier", ErrAccountSnapshotInvalid, accountID)
	}
	return AccountSnapshot{AccountID: accountID, RateMultiplier: rate}, nil
}

func resolveAccountSnapshotHTTPConfig() (string, string, string, error) {
	baseURL := strings.TrimSpace(os.Getenv(accountSnapshotBaseURLEnv))
	apiKey := strings.TrimSpace(os.Getenv(accountSnapshotAdminAPIKeyEnv))

	accountSnapshotHTTPState.RLock()
	cached := accountSnapshotHTTPState.config
	accountSnapshotHTTPState.RUnlock()
	if baseURL == "" {
		baseURL = strings.TrimSpace(cached.baseURL)
	}
	if baseURL == "" {
		baseURL = defaultAccountSnapshotBaseURL()
	}
	baseURL, err := normalizeAccountSnapshotBaseURL(baseURL)
	if err != nil {
		return "", "", "", err
	}

	switch {
	case apiKey != "":
		return baseURL, accountSnapshotAdminAPIKeyHeader, apiKey, nil
	case strings.TrimSpace(cached.adminAPIKey) != "":
		return baseURL, accountSnapshotAdminAPIKeyHeader, cached.adminAPIKey, nil
	case strings.TrimSpace(cached.authorization) != "":
		return baseURL, "Authorization", cached.authorization, nil
	default:
		return "", "", "", ErrAccountSnapshotAuthMissing
	}
}

func accountSnapshotBaseURLFromRequest(request *http.Request) string {
	if request == nil || strings.TrimSpace(request.Host) == "" {
		return ""
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	baseURL, err := normalizeAccountSnapshotBaseURL(scheme + "://" + request.Host)
	if err != nil {
		return ""
	}
	return baseURL
}

func defaultAccountSnapshotBaseURL() string {
	host := strings.TrimSpace(os.Getenv("SERVER_HOST"))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(os.Getenv("SERVER_PORT"))
	if port == "" {
		port = accountSnapshotDefaultPort
	}
	return "http://" + host + ":" + port
}

func reusableAdminAuthorization(request *http.Request) string {
	if request == nil {
		return ""
	}
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	tokenParts := strings.Split(parts[1], ".")
	if len(tokenParts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(tokenParts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		BindingHash string `json:"bnd"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || strings.TrimSpace(claims.BindingHash) != "" {
		return ""
	}
	return authorization
}

func normalizeAccountSnapshotBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid billing strategy base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Host) == "" {
		return "", errors.New("invalid billing strategy base URL")
	}
	if parsed.User != nil {
		return "", errors.New("invalid billing strategy base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}
