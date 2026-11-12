package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 卡有过期时间（DIRECT 卡通常随账单周期作废，GLOBAL 卡到自然月底），过期
// 作废——管理员需要在过期前规划使用。
//
// 端点（Authorization 直接携带 API key，与额度探测一致）：
//
//	GET  {origin}/api/biz/customer-package-reset/list?targetType=PERSONAL
//	     团队版：?targetType=TEAM + bigmodel-organization / bigmodel-project 头
//	POST {origin}/api/biz/customer-package-reset/use
//	     {targetType, resetType: FIVE_HOUR|WEEK, recordId, grantType, requestId: UUID}
//
// 参考 magpie #5349（yetone 已 review 的 8ab0685 实现）的防重复语义：消耗前
// 先写预留（requestId 固定），不确定结果（超时/网络错）不重试——卡可能已被
// 扣，宁可错过不重复扣。本网关为管理端手动操作，不做自动耗卡。
//
// 快照落 account.Extra["zhipu_reset_cards"]（list 结果）与
// account.Extra["zhipu_reset_used_log"]（最近耗卡审计，环形 20 条）。

const (
	zhipuResetListPath = "/api/biz/customer-package-reset/list"
	zhipuResetUsePath  = "/api/biz/customer-package-reset/use"
	zhipuResetTimeout  = 30 * time.Second

	zhipuExtraKeyResetCards = "zhipu_reset_cards"
	zhipuExtraKeyResetLog   = "zhipu_reset_used_log"
	zhipuResetLogMaxEntries = 20
)

// zhipuResetCard 一张重置卡。
type zhipuResetCard struct {
	RecordID   int64  `json:"recordId"`
	GrantType  string `json:"grantType"`
	ExpireTime string `json:"expireTime"` // "2006-01-02 15:04:05"（UTC+8）
	Available  bool   `json:"available"`
}

// ZhipuResetCardView 面板展示视图（含派生字段）。
type ZhipuResetCardView struct {
	RecordID    int64  `json:"recordId"`
	GrantType   string `json:"grantType"`
	ExpireTime  string `json:"expireTime"`
	ExpireAt    string `json:"expireAt"` // RFC3339（保留原始时区语义转 UTC）
	Available   bool   `json:"available"`
	Expired     bool   `json:"expired"`     // 派生：now > expireAt
	ExpiringInH int    `json:"expiringInH"` // 派生：距过期小时数（下取整，过期为 0）
	Window      string `json:"window"`      // 派生："5h" | "week"
}

// ZhipuResetCardsResult 重置卡面板数据。
type ZhipuResetCardsResult struct {
	TargetType            string               `json:"targetType"`
	LastFiveHourResetTime string               `json:"lastFiveHourResetTime,omitempty"`
	LastWeekResetTime     string               `json:"lastWeekResetTime,omitempty"`
	FiveHourCards         []ZhipuResetCardView `json:"fiveHourCards"`
	WeekCards             []ZhipuResetCardView `json:"weekCards"`
	FetchedAt             int64                `json:"fetchedAt"`
}

// zhipuResetUseRecord 耗卡审计条目。
type zhipuResetUseRecord struct {
	UsedAt    string `json:"usedAt"` // RFC3339
	Window    string `json:"window"` // FIVE_HOUR | WEEK
	RecordID  int64  `json:"recordId"`
	GrantType string `json:"grantType"`
	RequestID string `json:"requestId"`
	Operator  string `json:"operator"` // 管理员用户名
	Success   bool   `json:"success"`
	Detail    string `json:"detail,omitempty"`
}

// zhipuResetTarget 解析账号的重置卡查询形态（个人/团队）。
func zhipuResetTarget(account *Account) (targetType, org, project string) {
	org = strings.TrimSpace(account.GetCredential("zhipu_organization"))
	project = strings.TrimSpace(account.GetCredential("zhipu_project"))
	if org != "" {
		return "TEAM", org, project
	}
	return "PERSONAL", "", ""
}

// QueryZhipuResetCards 拉取重置卡列表并落快照。
func (s *CNProviderQuotaService) QueryZhipuResetCards(ctx context.Context, accountID int64) (*ZhipuResetCardsResult, error) {
	account, err := s.loadCodingPlanAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformZhipu {
		return nil, errors.New(http.StatusBadRequest, "ZHIPU_RESET_NOT_ZHIPU", "reset cards are only available on zhipu accounts")
	}
	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, errors.New(http.StatusBadRequest, "ZHIPU_RESET_NO_APIKEY", "account api_key is empty")
	}

	targetType, _, _ := zhipuResetTarget(account)
	listURL := zhipuQuotaHost(account.GetOpenAIBaseURL()) + zhipuResetListPath + "?targetType=" + targetType

	body, err := s.zhipuResetCall(ctx, account, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	if code := gjson.GetBytes(body, "code").Int(); code != 200 {
		msg := strings.TrimSpace(gjson.GetBytes(body, "msg").String())
		return nil, errors.Newf(http.StatusBadGateway, "ZHIPU_RESET_LIST_FAILED", "list cards failed: code=%d msg=%s", code, msg)
	}

	data := gjson.GetBytes(body, "data")
	result := &ZhipuResetCardsResult{
		TargetType:            targetType,
		LastFiveHourResetTime: data.Get("lastFiveHourResetTime").String(),
		LastWeekResetTime:     data.Get("lastWeekResetTime").String(),
		FetchedAt:             time.Now().UTC().Unix(),
	}
	now := time.Now()
	result.FiveHourCards = zhipuParseResetCards(data.Get("fiveHourResets"), "5h", now)
	result.WeekCards = zhipuParseResetCards(data.Get("weekResets"), "week", now)

	// 快照（管理员面板离线可见）。
	if raw, err := json.Marshal(map[string]any{
		"target_type": targetType,
		"fetched_at":  result.FetchedAt,
		"five_hour":   result.FiveHourCards,
		"week":        result.WeekCards,
	}); err == nil {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{zhipuExtraKeyResetCards: json.RawMessage(raw)})
	}
	return result, nil
}

// GetZhipuResetCardsSnapshot 读快照（不发上游请求）。
func (s *CNProviderQuotaService) GetZhipuResetCardsSnapshot(ctx context.Context, accountID int64) (map[string]any, error) {
	account, err := s.loadCodingPlanAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if snap, ok := account.Extra[zhipuExtraKeyResetCards].(map[string]any); ok {
		return snap, nil
	}
	return nil, errors.New(http.StatusNotFound, "ZHIPU_RESET_NO_SNAPSHOT", "no reset card snapshot; query the account first")
}

// UseZhipuResetCard 消耗一张重置卡。window: "5h"|"week"；recordID 传 0 时
// 自动选「最早过期且可用」的对应类型卡。
func (s *CNProviderQuotaService) UseZhipuResetCard(ctx context.Context, accountID int64, window string, recordID int64, operator string) (*ZhipuResetCardsResult, error) {
	account, err := s.loadCodingPlanAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformZhipu {
		return nil, errors.New(http.StatusBadRequest, "ZHIPU_RESET_NOT_ZHIPU", "reset cards are only available on zhipu accounts")
	}
	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, errors.New(http.StatusBadRequest, "ZHIPU_RESET_NO_APIKEY", "account api_key is empty")
	}
	var resetType string
	switch window {
	case "5h":
		resetType = "FIVE_HOUR"
	case "week":
		resetType = "WEEK"
	default:
		return nil, errors.New(http.StatusBadRequest, "ZHIPU_RESET_BAD_WINDOW", "window must be \"5h\" or \"week\"")
	}

	// 先拉最新列表（卡状态可能已变），再选卡。
	list, err := s.QueryZhipuResetCards(ctx, accountID)
	if err != nil {
		return nil, err
	}
	var candidates []ZhipuResetCardView
	if window == "5h" {
		candidates = list.FiveHourCards
	} else {
		candidates = list.WeekCards
	}
	var selected *ZhipuResetCardView
	for i := range candidates {
		c := &candidates[i]
		if !c.Available || c.Expired {
			continue
		}
		if recordID > 0 {
			if c.RecordID == recordID {
				selected = c
				break
			}
			continue
		}
		// 自动选：最早过期优先（快过期的卡先花，临期浪费最小）。
		if selected == nil || c.ExpireAt < selected.ExpireAt {
			selected = c
		}
	}
	if selected == nil {
		if recordID > 0 {
			return nil, errors.Newf(http.StatusBadRequest, "ZHIPU_RESET_CARD_NOT_FOUND", "card %d is not an available unexpired %s card", recordID, window)
		}
		return nil, errors.Newf(http.StatusConflict, "ZHIPU_RESET_NO_CARD", "no available unexpired %s reset card", window)
	}

	targetType, _, _ := zhipuResetTarget(account)
	requestID := newZhipuRequestID()
	useURL := zhipuQuotaHost(account.GetOpenAIBaseURL()) + zhipuResetUsePath
	payload, _ := json.Marshal(map[string]any{
		"targetType": targetType,
		"resetType":  resetType,
		"recordId":   selected.RecordID,
		"grantType":  selected.GrantType,
		"requestId":  requestID,
	})

	record := zhipuResetUseRecord{
		UsedAt:    time.Now().UTC().Format(time.RFC3339),
		Window:    resetType,
		RecordID:  selected.RecordID,
		GrantType: selected.GrantType,
		RequestID: requestID,
		Operator:  operator,
	}
	body, callErr := s.zhipuResetCall(ctx, account, http.MethodPost, useURL, payload)
	if callErr != nil {
		record.Detail = callErr.Error()
		s.appendZhipuResetLog(ctx, account.ID, record)
		// 不确定结果（超时/5xx）：卡可能已被扣。管理端如实报错，由管理员
		// 刷新列表后决策；不做自动重试（同 magpie 预留语义）。
		return nil, callErr
	}
	if code := gjson.GetBytes(body, "code").Int(); code != 200 {
		msg := strings.TrimSpace(gjson.GetBytes(body, "msg").String())
		record.Detail = fmt.Sprintf("code=%d msg=%s", code, msg)
		s.appendZhipuResetLog(ctx, account.ID, record)
		return nil, errors.Newf(http.StatusBadGateway, "ZHIPU_RESET_USE_FAILED", "use card failed: code=%d msg=%s", code, msg)
	}
	record.Success = true
	s.appendZhipuResetLog(ctx, account.ID, record)

	// 耗卡后立即刷新用量快照（窗口应清零），调度恢复。
	if _, err := s.QueryUsage(ctx, accountID); err != nil {
		slog.Error("zhipu_reset_usage_refresh_failed", "account_id", accountID, "error", err)
	}
	return s.QueryZhipuResetCards(ctx, accountID)
}

// zhipuResetCall 发起重置卡 API 调用（复用额度探测的出站策略与代理）。
func (s *CNProviderQuotaService) zhipuResetCall(ctx context.Context, account *Account, method, url string, payload []byte) ([]byte, error) {
	validatedURL, err := cnValidateProbeURL(s.cfg, url)
	if err != nil {
		return nil, errors.New(http.StatusForbidden, "ZHIPU_RESET_URL_REJECTED", err.Error())
	}
	proxyURL := s.resolveProxyURL(ctx, account)
	callCtx, cancel := context.WithTimeout(ctx, zhipuResetTimeout)
	defer cancel()

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(callCtx, method, validatedURL, reader)
	if err != nil {
		return nil, errors.Newf(http.StatusInternalServerError, "ZHIPU_RESET_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", strings.TrimSpace(account.GetCNAPIKey()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "en-US,en")
	if _, org, project := zhipuResetTarget(account); org != "" {
		req.Header.Set("bigmodel-organization", org)
		if project != "" {
			req.Header.Set("bigmodel-project", project)
		}
	}
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, errors.Newf(http.StatusBadGateway, "ZHIPU_RESET_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, cnQuotaMaxBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf(http.StatusBadGateway, "ZHIPU_RESET_HTTP_ERROR", "HTTP %d: %s", resp.StatusCode, truncate(string(body), 240))
	}
	return body, nil
}

// zhipuParseResetCards 解析卡列表并派生临期字段。expireTime 为 UTC+8 文本。
func zhipuParseResetCards(cards gjson.Result, window string, now time.Time) []ZhipuResetCardView {
	if !cards.IsArray() {
		return []ZhipuResetCardView{}
	}
	zone := time.FixedZone("UTC+8", 8*3600)
	out := make([]ZhipuResetCardView, 0, len(cards.Array()))
	for _, c := range cards.Array() {
		v := ZhipuResetCardView{
			RecordID:   c.Get("recordId").Int(),
			GrantType:  c.Get("grantType").String(),
			ExpireTime: c.Get("expireTime").String(),
			Available:  c.Get("available").Bool(),
			Window:     window,
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", v.ExpireTime, zone); err == nil {
			utc := t.UTC()
			v.ExpireAt = utc.Format(time.RFC3339)
			if utc.After(now) {
				v.ExpiringInH = int(utc.Sub(now).Hours())
			} else {
				v.Expired = true
			}
		} else {
			// 无法解析过期时间的卡标为不可用（保守）。
			v.Expired = true
		}
		out = append(out, v)
	}
	return out
}

// appendZhipuResetLog 追加耗卡审计（环形 20 条）。
func (s *CNProviderQuotaService) appendZhipuResetLog(ctx context.Context, accountID int64, record zhipuResetUseRecord) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return
	}
	var logEntries []zhipuResetUseRecord
	if raw, ok := account.Extra[zhipuExtraKeyResetLog].([]any); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				b, _ := json.Marshal(m)
				var r zhipuResetUseRecord
				if json.Unmarshal(b, &r) == nil {
					logEntries = append(logEntries, r)
				}
			}
		}
	}
	logEntries = append(logEntries, record)
	if len(logEntries) > zhipuResetLogMaxEntries {
		logEntries = logEntries[len(logEntries)-zhipuResetLogMaxEntries:]
	}
	b, _ := json.Marshal(logEntries)
	_ = s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{zhipuExtraKeyResetLog: json.RawMessage(b)})
}

// newZhipuRequestID 生成耗卡幂等请求 ID（UUID v4）。
func newZhipuRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ZhipuResetCardsFromExtra 供 handler 读快照的转换辅助。
func ZhipuResetCardsFromExtra(extra map[string]any) map[string]any {
	if snap, ok := extra[zhipuExtraKeyResetCards].(map[string]any); ok {
		return snap
	}
	return nil
}
