// Package plugin bootstraps the in-process plugin registry.
//
// Go does not expose a registry of already-compiled functions. The registry
// used here is an explicit, typed dispatch boundary owned by the application.
// Host call sites invoke stable plugin facades below, while plugin
// implementations can be replaced during startup without changing those call
// sites.
//
// 当前状态：空注册表。历史功能插件（模型映射/IQ 路由/额度保护/计费策略/
// 任务中心/Claw163 通知）已移除，facade 全部退化为 no-op：
//   - 请求转发不做模型/强度改写（TransformRequest 直接返回）
//   - 计费回退上游原生行为：EffectiveBillingModelSource 永远返回调用方
//     传入值（不启用 max-cost），模型解析原样透传，倍率恒为 1
//   - 用量/成本记录丢弃（无插件账本）
//   - 管理路由不注册
//
// 后续如需新插件：在 internal/plugin/<name>/ 实现并在此注册，宿主调用点
// 无需改动。
package plugin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var errPluginRemoved = errors.New("plugin removed: notification channels are no longer available")

// Install installs all built-in plugin implementations. It is intentionally
// idempotent so startup and tests can call it more than once.
func Install() {}

// RegisterAdminRoutes registers all built-in plugin-owned admin endpoints.
func RegisterAdminRoutes(_ *gin.RouterGroup) {}

// AdminJobNotification is retained for host-side notifier wiring.
type AdminJobNotification struct {
	Title   string
	Body    string
	Subject string
}

// AdminJobNotifier connects the (removed) task-center plugin to a notification
// provider. No implementation is registered; calls never happen.
type AdminJobNotifier interface {
	Send(ctx context.Context, notification AdminJobNotification) error
}

// RegisterAdminJobNotifier is a no-op retention point for future plugins.
func RegisterAdminJobNotifier(_ AdminJobNotifier) {}

// SendClaw163Notification always fails: the notification channel was removed.
func SendClaw163Notification(_ context.Context, _, _ string) error {
	return errPluginRemoved
}

// BillingModelSourceMaxCost is the historical max-cost source constant.
const BillingModelSourceMaxCost = "max_cost"

// BillingStrategyInput is retained so max-cost call sites keep compiling.
type BillingStrategyInput struct {
	Context       context.Context
	AccountID     int64
	Effort        string
	MappingChain  string
	Models        []string
	PricingSource func(string) string
	Evaluate      func(string) (BillingAuditCandidate, error)
}

// BillingAuditCandidate describes one pricing candidate (historical shape).
type BillingAuditCandidate struct {
	Model           string
	Available       bool
	Error           string
	BillingMode     string
	InputCost       float64
	ImageInputCost  float64
	OutputCost      float64
	ImageOutputCost float64
	CacheWriteCost  float64
	CacheReadCost   float64
	TotalCost       float64
}

// BillingStrategyDecision is the historical decision snapshot shape.
type BillingStrategyDecision struct {
	AccountID               int64
	Strategy                string
	MappingChain            string
	Effort                  string
	SelectedModel           string
	SelectedCost            BillingAuditCandidate
	VirtualModelCost        float64
	AccountRateMultiplier   float64
	AccountSnapshotResolved bool
	AccountSnapshotError    error
	Reason                  string
	Candidates              []BillingAuditCandidate
}

// BillingAuditRecord is the historical audit entry shape.
type BillingAuditRecord struct {
	RequestID             string
	AccountID             int64
	RequestedModel        string
	MappingChain          string
	Strategy              string
	SelectedModel         string
	InputTokens           int
	OutputTokens          int
	CacheCreationTokens   int
	CacheReadTokens       int
	ImageInputTokens      int
	ImageOutputTokens     int
	ImageCount            int
	GroupRateMultiplier   float64
	AccountRateMultiplier float64
	TotalCost             float64
	ActualCost            float64
	AccountStatsCost      *float64
	AccountBilledCost     float64
	Candidates            []BillingAuditCandidate
}

// WithBillingStrategyAccountRateMultiplier is a no-op pass-through.
func WithBillingStrategyAccountRateMultiplier(ctx context.Context, _ int64, _ float64) context.Context {
	return ctx
}

// ResolveMaxCostBillingDecision always reports "no candidate available" so the
// host falls back to native billing.
func ResolveMaxCostBillingDecision(input BillingStrategyInput) BillingStrategyDecision {
	return BillingStrategyDecision{Strategy: "disabled", Reason: "plugin registry is empty"}
}

// ResolveMaxCostBillingDecisionWithAccountSnapshot mirrors the disabled decision.
func ResolveMaxCostBillingDecisionWithAccountSnapshot(ctx context.Context, input BillingStrategyInput) (BillingStrategyDecision, error) {
	return ResolveMaxCostBillingDecision(input), nil
}

// ResolveBillingModel passes the requested model through unchanged.
func ResolveBillingModel(_ int64, model, _ string) string { return model }

// ApplyAccountBillingModelSource no longer mutates the billing source.
func ApplyAccountBillingModelSource(_ map[string]any, _ *string) {}

// EffectiveBillingModelSource returns the caller-provided source: max-cost is
// never activated without a plugin.
func EffectiveBillingModelSource(_ map[string]any, source string) string { return source }

// BillingRate applies the historical mode-aware rate formula, which is pure
// arithmetic and stays host-usable without any plugin.
func BillingRate(billingMode string, tokenRate, imageRate, videoRate, searchRate float64, imageCount, videoCount, searchCalls int) float64 {
	rate := tokenRate
	if needsImageRate(billingMode) && imageRate > 0 {
		rate = imageRate
	}
	if billingMode == "video" && videoRate > 0 {
		rate = videoRate
	}
	if billingMode == "search" && searchRate > 0 {
		rate = searchRate
	}
	rate += float64(imageCount)*imageRate + float64(videoCount)*videoRate + float64(searchCalls)*searchRate
	return rate
}

func needsImageRate(billingMode string) bool {
	return billingMode == "image" || billingMode == "image_generation"
}

// CalculateUserChargeCost multiplies by the group rate (identity when 0).
func CalculateUserChargeCost(rawCost, rateMultiplier float64) float64 {
	if rateMultiplier <= 0 {
		return rawCost
	}
	return rawCost * rateMultiplier
}

// CalculateAccountUserChargeCost multiplies by the account rate (identity when 0).
func CalculateAccountUserChargeCost(rawCost, accountRateMultiplier float64) float64 {
	if accountRateMultiplier <= 0 {
		return rawCost
	}
	return rawCost * accountRateMultiplier
}

// ApplyVirtualBillingCost is retained as a no-op; virtual cost blending was
// plugin-owned.
func ApplyVirtualBillingCost(_, _ float64, _ *float64, _ ...*float64) {}

// RecordBillingAudit drops the entry: no plugin ledger exists.
func RecordBillingAudit(_ BillingAuditRecord) {}

// SelectMostExpensiveBillingCandidate reports "not available".
func SelectMostExpensiveBillingCandidate(_ []BillingAuditCandidate) (int, bool) {
	return -1, false
}

// TransformRequest no longer rewrites requests.
func TransformRequest(_ *http.Request, _ int64) {}

// UsageMapping is the historical mapping result shape.
type UsageMapping struct {
	Matched    bool
	Model      string
	ModelFound bool
}

// RecordUsageMappingFromResult drops plugin-owned mapping statistics.
func RecordUsageMappingFromResult(_ int64, _ string, _ *string, _, _ int) {}

// ResolveUsageWithEffort reports no mapping.
func ResolveUsageWithEffort(_ int64, _, _ string) UsageMapping {
	return UsageMapping{}
}

// UpstreamUsageTotals is the historical ledger totals shape.
type UpstreamUsageTotals struct {
	CostUSD     float64
	TotalTokens int64
}

// RecordUpstreamUsage drops provider-side ledger entries.
func RecordUpstreamUsage(_ int64, _ float64, _ int64) {}

// RecordUpstreamCost drops provider-side cost entries.
func RecordUpstreamCost(_ int64, _ float64) {}

// DailyUpstreamUsage reports zeros.
func DailyUpstreamUsage(_ int64, _ string, _ time.Time) UpstreamUsageTotals {
	return UpstreamUsageTotals{}
}

// DailyUpstreamCost reports zero.
func DailyUpstreamCost(_ int64, _ string, _ time.Time) float64 { return 0 }

// WeeklyUpstreamUsage reports zeros.
func WeeklyUpstreamUsage(_ int64, _ string, _ time.Time) UpstreamUsageTotals {
	return UpstreamUsageTotals{}
}
