// Package plugin bootstraps the in-process plugin registry.
//
// Go does not expose a registry of already-compiled functions. The registry
// used here is an explicit, typed dispatch boundary owned by the application.
// Host call sites invoke stable plugin facades, while plugin implementations
// can be replaced during startup without changing those call sites.
package plugin

import (
	"context"
	"net/http"
	"time"

	billing_strategy "github.com/Wei-Shaw/sub2api/internal/plugin/billing_strategy"
	"github.com/Wei-Shaw/sub2api/internal/plugin/claw163"
	model_reasoning_effort "github.com/Wei-Shaw/sub2api/internal/plugin/model_reasoning_effort"
	"github.com/gin-gonic/gin"
)

// Install installs all built-in plugin implementations. It is intentionally
// idempotent so startup and tests can call it more than once.
func Install() {
	claw163.Install()
	model_reasoning_effort.Install()
}

// RegisterAdminRoutes registers all built-in plugin-owned admin endpoints.
// Host routing code depends on this stable facade, not on a feature plugin.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	claw163.RegisterAdminRoutes(adminGroup)
	model_reasoning_effort.RegisterAdminRoutes(adminGroup)
	billing_strategy.RegisterAdminRoutes(adminGroup)
}

// SendClaw163Notification is the host-facing notification facade. Recipient,
// profile, credential, and CLI details remain owned by the plugin.
func SendClaw163Notification(ctx context.Context, subject, body string) error {
	return claw163.Send(ctx, subject, body)
}

type BillingAuditCandidate = billing_strategy.Candidate
type BillingAuditRecord = billing_strategy.AuditRecord
type BillingStrategyDecision = billing_strategy.Decision
type BillingStrategyInput = billing_strategy.DecisionInput

const BillingModelSourceMaxCost = billing_strategy.BillingModelSourceMaxCost

// ResolveMaxCostBillingDecision keeps model candidate expansion and selection
// inside the billing-strategy plugin.
func ResolveMaxCostBillingDecision(input BillingStrategyInput) BillingStrategyDecision {
	if input.Context != nil {
		decision, err := billing_strategy.ResolveMaxCostDecisionWithAccountSnapshot(input.Context, input)
		decision.AccountSnapshotError = err
		return decision
	}
	return billing_strategy.ResolveMaxCostDecision(input)
}

// ResolveMaxCostBillingDecisionWithAccountSnapshot keeps account-rate lookup
// inside the plugin. The host supplies only the request context and account ID;
// the plugin calls the existing admin account endpoint and returns one immutable
// decision snapshot for pricing and audit.
func ResolveMaxCostBillingDecisionWithAccountSnapshot(ctx context.Context, input BillingStrategyInput) (BillingStrategyDecision, error) {
	return billing_strategy.ResolveMaxCostDecisionWithAccountSnapshot(ctx, input)
}

func ResolveBillingModel(accountID int64, model, effort string) string {
	return billing_strategy.ResolveBillingModel(accountID, model, effort)
}

func ApplyAccountBillingModelSource(extra map[string]any, source *string) {
	billing_strategy.ApplyAccountBillingModelSource(extra, source)
}

func EffectiveBillingModelSource(extra map[string]any, channelSource string) string {
	return billing_strategy.EffectiveBillingModelSource(extra, channelSource)
}

func BillingRate(billingMode string, tokenRate, imageRate, videoRate, searchRate float64, imageCount, videoCount, searchCalls int) float64 {
	return billing_strategy.BillingRate(billingMode, tokenRate, imageRate, videoRate, searchRate, imageCount, videoCount, searchCalls)
}

func CalculateUserChargeCost(rawCost, rateMultiplier float64) float64 {
	return billing_strategy.CalculateUserChargeCost(rawCost, rateMultiplier)
}

func ApplyVirtualBillingCost(rawTotal, virtualTotal float64, totalCost, actualCost *float64, components ...*float64) {
	billing_strategy.ApplyVirtualBillingCost(rawTotal, virtualTotal, totalCost, actualCost, components...)
}

// RecordBillingAudit writes the plugin-owned explanation of a max-cost billing
// decision. It never changes the host billing result.
func RecordBillingAudit(record BillingAuditRecord) {
	billing_strategy.Record(record)
}

// SelectMostExpensiveBillingCandidate keeps the strategy decision in the
// plugin boundary while price resolution remains in the host BillingService.
func SelectMostExpensiveBillingCandidate(candidates []BillingAuditCandidate) (int, bool) {
	return billing_strategy.SelectMostExpensive(candidates)
}

// TransformRequest dispatches the process-wide request transformation hook.
func TransformRequest(req *http.Request, accountID int64) {
	model_reasoning_effort.TransformRequest(req, accountID)
}

// RecordUsageMappingFromResult dispatches plugin-owned mapping statistics.
func RecordUsageMappingFromResult(accountID int64, model string, effort *string, inputTokens, outputTokens int) {
	model_reasoning_effort.RecordUsageMappingFromResult(accountID, model, effort, inputTokens, outputTokens)
}

// ResolveUsageWithEffort returns the same source-model + source-effort mapping
// used by the request transformer. Callers can use the effective model for
// provider-side accounting without changing the user-facing billing model.
func ResolveUsageWithEffort(accountID int64, model, effort string) model_reasoning_effort.UsageMapping {
	return model_reasoning_effort.ResolveUsageWithEffort(accountID, model, effort)
}

// RecordUpstreamUsage records provider-side standard cost and tokens in the
// plugin-owned ledger. User billing remains in the host usage log.
func RecordUpstreamUsage(accountID int64, costUSD float64, totalTokens int64) {
	model_reasoning_effort.RecordUpstreamUsage(accountID, costUSD, totalTokens)
}

// RecordUpstreamCost records provider-side standard cost for plugin-owned
// quota protection. It deliberately does not include user billing markup.
func RecordUpstreamCost(accountID int64, costUSD float64) {
	model_reasoning_effort.RecordUpstreamCost(accountID, costUSD)
}

// DailyUpstreamUsage returns provider-side cost and tokens for the current day.
func DailyUpstreamUsage(accountID int64, timezoneName string, now time.Time) model_reasoning_effort.UpstreamUsageTotals {
	return model_reasoning_effort.DailyUpstreamUsage(accountID, timezoneName, now)
}

// DailyUpstreamCost returns the provider-side spend for the current calendar
// day in the requested timezone.
func DailyUpstreamCost(accountID int64, timezoneName string, now time.Time) float64 {
	return model_reasoning_effort.DailyUpstreamCost(accountID, timezoneName, now)
}

// WeeklyUpstreamUsage returns provider-side cost and tokens for the local
// calendar week used by quota protection.
func WeeklyUpstreamUsage(accountID int64, timezoneName string, now time.Time) model_reasoning_effort.UpstreamUsageTotals {
	return model_reasoning_effort.WeeklyUpstreamUsage(accountID, timezoneName, now)
}
