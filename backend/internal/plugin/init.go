// Package plugin bootstraps the in-process plugin registry.
//
// Go does not expose a registry of already-compiled functions. The registry
// used here is an explicit, typed dispatch boundary owned by the application.
// Host call sites invoke stable plugin facades, while plugin implementations
// can be replaced during startup without changing those call sites.
package plugin

import (
	"net/http"
	"time"

	model_reasoning_effort "github.com/Wei-Shaw/sub2api/internal/plugin/model_reasoning_effort"
	"github.com/gin-gonic/gin"
)

// Install installs all built-in plugin implementations. It is intentionally
// idempotent so startup and tests can call it more than once.
func Install() {
	model_reasoning_effort.Install()
}

// RegisterAdminRoutes registers all built-in plugin-owned admin endpoints.
// Host routing code depends on this stable facade, not on a feature plugin.
func RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	model_reasoning_effort.RegisterAdminRoutes(adminGroup)
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
