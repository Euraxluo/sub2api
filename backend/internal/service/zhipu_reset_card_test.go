package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

// zhipuResetStubServer 模拟官方重置卡两端点 + 额度端点（耗卡后刷新）。
type zhipuResetStubServer struct {
	mu sync.Mutex
	// 卡列表状态
	fiveHour []map[string]any
	week     []map[string]any
	// 请求记录
	listQueries    []string
	useBodies      []map[string]any
	orgHeaders     []string
	projectHeaders []string
	// use 行为控制
	useFailCode int
}

func newZhipuResetStubServer() *zhipuResetStubServer {
	return &zhipuResetStubServer{
		fiveHour: []map[string]any{
			{"recordId": float64(1975189), "grantType": "DIRECT", "available": true, "expireTime": "2099-11-14 09:31:33"},
		},
		week: []map[string]any{
			{"recordId": float64(9), "grantType": "GLOBAL", "available": true, "expireTime": "2098-12-22 23:59:59"},
			{"recordId": float64(10), "grantType": "GLOBAL", "available": true, "expireTime": "2099-12-22 23:59:59"},
		},
	}
}

func (s *zhipuResetStubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/api/biz/customer-package-reset/list":
		s.listQueries = append(s.listQueries, r.URL.RawQuery)
		s.orgHeaders = append(s.orgHeaders, r.Header.Get("bigmodel-organization"))
		s.projectHeaders = append(s.projectHeaders, r.Header.Get("bigmodel-project"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "操作成功",
			"data": map[string]any{
				"customerId":            float64(7601757164150851),
				"targetType":            "PERSONAL",
				"lastFiveHourResetTime": "2026-11-09 17:07:19",
				"lastWeekResetTime":     "2026-11-10 15:19:28",
				"fiveHourResets":        s.fiveHour,
				"weekResets":            s.week,
			},
		})
	case "/api/biz/customer-package-reset/use":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.useBodies = append(s.useBodies, body)
		if s.useFailCode != 0 {
			w.WriteHeader(s.useFailCode)
			_, _ = w.Write([]byte(`{"code":500,"msg":"boom"}`))
			return
		}
		// 模拟扣卡：从对应列表移除。
		id := int64(body["recordId"].(float64))
		if body["resetType"] == "FIVE_HOUR" {
			s.fiveHour = removeCard(s.fiveHour, id)
		} else {
			s.week = removeCard(s.week, id)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "success": true, "data": id})
	case "/api/monitor/usage/quota/limit":
		// 额度端点（耗卡后 QueryUsage 刷新用）。
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"limits": []map[string]any{
				{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 0, "nextResetTime": time.Now().Add(time.Hour).UnixMilli()},
			}},
		})
	default:
		w.WriteHeader(404)
	}
}

func removeCard(cards []map[string]any, id int64) []map[string]any {
	out := cards[:0]
	for _, c := range cards {
		if int64(c["recordId"].(float64)) != id {
			out = append(out, c)
		}
	}
	return out
}

// zhipuResetTestEnv 组装服务 + stub。
type zhipuResetTestEnv struct {
	svc  *CNProviderQuotaService
	repo *fakeCNResetAccountRepo
	stub *zhipuResetStubServer
	srv  *httptest.Server
}

type fakeCNResetAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *fakeCNResetAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.account, nil
}

func (r *fakeCNResetAccountRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	for k, v := range updates {
		if raw, ok := v.(json.RawMessage); ok {
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				r.account.Extra[k] = parsed
				continue
			}
		}
		r.account.Extra[k] = v
	}
	return nil
}

func newZhipuResetTestEnv(t *testing.T, account *Account) *zhipuResetTestEnv {
	t.Helper()
	stub := newZhipuResetStubServer()
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	// base_url 指向本地 stub 主机（zhipuQuotaHost 会取其 host）。
	if account.Credentials == nil {
		account.Credentials = map[string]any{}
	}
	account.Credentials["base_url"] = srv.URL + "/api/coding/paas/v4"
	env := &zhipuResetTestEnv{
		repo: &fakeCNResetAccountRepo{account: account},
		stub: stub,
		srv:  srv,
	}
	stubHost := strings.TrimPrefix(srv.URL, "http://")
	env.svc = &CNProviderQuotaService{
		accountRepo:  env.repo,
		httpUpstream: redirectUpstream{scheme: "http", host: stubHost},
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}
	return env
}

// redirectUpstream 把官方域请求重定向到本地 stub（生产 zhipuQuotaHost 固定
// 返回官方域，测试在传输层改写 host，语义与 magpie glmResetTransport 相同）。
type redirectUpstream struct{ scheme, host string }

func (u redirectUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	req.URL.Scheme, req.URL.Host = u.scheme, u.host
	return http.DefaultTransport.RoundTrip(req)
}

func (u redirectUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	req.URL.Scheme, req.URL.Host = u.scheme, u.host
	return http.DefaultTransport.RoundTrip(req)
}

func zhipuResetTestAccount() *Account {
	return &Account{
		ID: 1, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Status: StatusActive,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "testkey.testsecret",
			"account_mode": AccountModeCoding,
		},
	}
}

func TestZhipuResetCardsListPersonal(t *testing.T) {
	env := newZhipuResetTestEnv(t, zhipuResetTestAccount())

	result, err := env.svc.QueryZhipuResetCards(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "PERSONAL", result.TargetType)
	require.Len(t, result.FiveHourCards, 1)
	require.Len(t, result.WeekCards, 2)
	require.Equal(t, int64(1975189), result.FiveHourCards[0].RecordID)
	require.False(t, result.FiveHourCards[0].Expired)
	require.Equal(t, "5h", result.FiveHourCards[0].Window)
	require.Equal(t, "week", result.WeekCards[0].Window)
	require.NotEmpty(t, result.LastWeekResetTime)

	// 请求形状：PERSONAL + 无团队头。
	env.stub.mu.Lock()
	require.Contains(t, env.stub.listQueries[0], "targetType=PERSONAL")
	require.Empty(t, env.stub.orgHeaders[0])
	env.stub.mu.Unlock()

	// 快照落 Extra。
	require.NotNil(t, env.repo.account.Extra[zhipuExtraKeyResetCards])
}

func TestZhipuResetCardsListTeam(t *testing.T) {
	account := zhipuResetTestAccount()
	account.Credentials["zhipu_organization"] = "org-test"
	account.Credentials["zhipu_project"] = "proj-test"
	env := newZhipuResetTestEnv(t, account)

	result, err := env.svc.QueryZhipuResetCards(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "TEAM", result.TargetType)

	env.stub.mu.Lock()
	require.Contains(t, env.stub.listQueries[0], "targetType=TEAM")
	require.Equal(t, "org-test", env.stub.orgHeaders[0])
	require.Equal(t, "proj-test", env.stub.projectHeaders[0])
	env.stub.mu.Unlock()
}

func TestZhipuResetCardExpiredAndParse(t *testing.T) {
	// 过期卡解析：ExpireAt 派生 + Expired=true。
	cards := zhipuParseResetCards(gjsonParse(`[{"recordId":1,"grantType":"GLOBAL","available":true,"expireTime":"2020-01-01 00:00:00"},{"recordId":2,"grantType":"GLOBAL","available":true,"expireTime":"2099-01-01 00:00:00"},{"recordId":3,"grantType":"GLOBAL","available":true,"expireTime":"garbage"}]`), "week", time.Now())
	require.Len(t, cards, 3)
	require.True(t, cards[0].Expired)
	require.Zero(t, cards[0].ExpiringInH)
	require.False(t, cards[1].Expired)
	require.Greater(t, cards[1].ExpiringInH, 0)
	// 无法解析 → 保守标过期。
	require.True(t, cards[2].Expired)
	require.Equal(t, "2019-12-31T16:00:00Z", cards[0].ExpireAt, "expireTime is UTC+8; 2020-01-01 00:00 +08 == 2019-12-31 16:00 UTC")
}

func TestZhipuResetUseWeekAutoEarliestExpiry(t *testing.T) {
	env := newZhipuResetTestEnv(t, zhipuResetTestAccount())

	result, err := env.svc.UseZhipuResetCard(context.Background(), 1, "week", 0, "admin:1")
	require.NoError(t, err)

	env.stub.mu.Lock()
	require.Len(t, env.stub.useBodies, 1)
	body := env.stub.useBodies[0]
	env.stub.mu.Unlock()
	require.Equal(t, "WEEK", body["resetType"])
	require.Equal(t, "PERSONAL", body["targetType"])
	require.Equal(t, float64(9), body["recordId"], "must pick earliest-expiring card")
	require.Equal(t, "GLOBAL", body["grantType"])
	requestID, _ := body["requestId"].(string)
	require.NotEmpty(t, requestID)

	// 扣卡后列表更新 + 快照 + 审计。
	require.Len(t, result.WeekCards, 1)
	require.Equal(t, int64(10), result.WeekCards[0].RecordID)
	require.NotNil(t, env.repo.account.Extra[zhipuExtraKeyResetLog])
	logRaw, _ := json.Marshal(env.repo.account.Extra[zhipuExtraKeyResetLog])
	require.Contains(t, string(logRaw), `"recordId":9`)
	require.Contains(t, string(logRaw), `"success":true`)
	require.Contains(t, string(logRaw), `"operator":"admin:1"`)
}

func TestZhipuResetUseSpecificRecord(t *testing.T) {
	env := newZhipuResetTestEnv(t, zhipuResetTestAccount())
	_, err := env.svc.UseZhipuResetCard(context.Background(), 1, "5h", 1975189, "admin:1")
	require.NoError(t, err)
	env.stub.mu.Lock()
	require.Equal(t, "FIVE_HOUR", env.stub.useBodies[0]["resetType"])
	env.stub.mu.Unlock()
}

func TestZhipuResetUseFailures(t *testing.T) {
	t.Run("bad window", func(t *testing.T) {
		env := newZhipuResetTestEnv(t, zhipuResetTestAccount())
		_, err := env.svc.UseZhipuResetCard(context.Background(), 1, "monthly", 0, "admin:1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "window")
	})
	t.Run("missing card", func(t *testing.T) {
		env := newZhipuResetTestEnv(t, zhipuResetTestAccount())
		_, err := env.svc.UseZhipuResetCard(context.Background(), 1, "5h", 999, "admin:1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an available")
	})
	t.Run("upstream business failure logged", func(t *testing.T) {
		env := newZhipuResetTestEnv(t, zhipuResetTestAccount())
		env.stub.mu.Lock()
		env.stub.useFailCode = 502
		env.stub.mu.Unlock()
		_, err := env.svc.UseZhipuResetCard(context.Background(), 1, "week", 0, "admin:1")
		require.Error(t, err)
		// 失败也进审计。
		logRaw, _ := json.Marshal(env.repo.account.Extra[zhipuExtraKeyResetLog])
		require.Contains(t, string(logRaw), `"success":false`)
		require.Contains(t, string(logRaw), `"operator":"admin:1"`)
	})
	t.Run("non-zhipu account rejected", func(t *testing.T) {
		account := zhipuResetTestAccount()
		account.Platform = PlatformKimi
		env := newZhipuResetTestEnv(t, account)
		_, err := env.svc.QueryZhipuResetCards(context.Background(), 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "only available on zhipu")
	})
}

// gjsonParse 用 gjson.Result 包装 JSON 文本（与生产解析同路径）。
func gjsonParse(s string) gjson.Result {
	return gjson.Parse(s)
}
