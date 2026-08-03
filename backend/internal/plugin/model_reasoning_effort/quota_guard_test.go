package model_reasoning_effort

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func quotaGuardTestAccount(id int64, schedulable bool, extra map[string]any) quotaGuardAccount {
	return quotaGuardAccount{
		ID:          id,
		Platform:    "deepseek",
		Type:        "api_key",
		Schedulable: schedulable,
		Extra:       extra,
	}
}

func quotaGuardTestPolicy(id string) quotaGuardConfig {
	return quotaGuardConfig{
		ID:                 id,
		Name:               id,
		Enabled:            true,
		IntervalSeconds:    60,
		AccountIDs:         []int64{1},
		DailySpendTimezone: "Asia/Shanghai",
	}
}

func TestEvaluateQuotaGuardDecisionUsesDailySpendWithoutProviderFilter(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	policy := quotaGuardTestPolicy("daily-cost")
	policy.DailySpendLimitUSD = 500

	decision, ok := evaluateQuotaGuardDecision(
		quotaGuardTestAccount(1, true, map[string]any{"codex_5h_used_percent": 100}),
		policy,
		now,
		UpstreamUsageTotals{CostUSD: 500},
		UpstreamUsageTotals{},
	)

	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.Contains(t, decision.Extra["codex_quota_guard_blocked_reason"], "daily_spend_limit")
	require.Equal(t, "2026-05-13T16:00:00Z", decision.Extra["codex_quota_guard_blocked_until"])
}

func TestEvaluateQuotaGuardDecisionUsesDailyTokens(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	policy := quotaGuardTestPolicy("daily-tokens")
	policy.DailyTokenLimit = 1000

	decision, ok := evaluateQuotaGuardDecision(
		quotaGuardTestAccount(1, true, nil),
		policy,
		now,
		UpstreamUsageTotals{Tokens: 1000},
		UpstreamUsageTotals{},
	)

	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.Contains(t, decision.Extra["codex_quota_guard_blocked_reason"], "daily_token_limit")
}

func TestEvaluateQuotaGuardDecisionUsesWeeklyLimits(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	policy := quotaGuardTestPolicy("weekly")
	policy.WeeklySpendLimitUSD = 50
	policy.WeeklyTokenLimit = 100

	decision, ok := evaluateQuotaGuardDecision(
		quotaGuardTestAccount(1, true, nil),
		policy,
		now,
		UpstreamUsageTotals{},
		UpstreamUsageTotals{CostUSD: 50, Tokens: 100},
	)

	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.Contains(t, decision.Extra["codex_quota_guard_blocked_reason"], "weekly_spend_limit")
	require.Contains(t, decision.Extra["codex_quota_guard_blocked_reason"], "weekly_token_limit")
	require.Equal(t, "2026-05-17T16:00:00Z", decision.Extra["codex_quota_guard_blocked_until"])
}

func TestEvaluateQuotaGuardDecisionReleasesUnselectedManagedAccount(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	policy := quotaGuardTestPolicy("selected-only")
	policy.AccountIDs = []int64{2}

	decision, ok := evaluateQuotaGuardDecision(
		quotaGuardTestAccount(1, false, map[string]any{"codex_quota_guard_managed": true}),
		policy,
		now,
		UpstreamUsageTotals{},
		UpstreamUsageTotals{},
	)

	require.True(t, ok)
	require.Equal(t, "release", decision.Action)
	require.Equal(t, false, decision.Extra["codex_quota_guard_managed"])
	require.Equal(t, false, decision.Extra["quota_guard_managed"])
}

func TestEvaluateQuotaGuardPoliciesCombinesLimiterGroups(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	daily := quotaGuardTestPolicy("daily")
	daily.DailySpendLimitUSD = 10
	weekly := quotaGuardTestPolicy("weekly")
	weekly.WeeklyTokenLimit = 100

	decision, ok := evaluateQuotaGuardPoliciesDecision(
		quotaGuardTestAccount(1, true, nil),
		[]quotaGuardConfig{daily, weekly},
		now,
		map[string]quotaGuardUsage{
			"Asia/Shanghai": {
				Daily:  UpstreamUsageTotals{CostUSD: 10},
				Weekly: UpstreamUsageTotals{Tokens: 100},
			},
		},
	)

	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.Contains(t, decision.Extra["codex_quota_guard_policy_ids"], "daily")
	require.Contains(t, decision.Extra["codex_quota_guard_policy_ids"], "weekly")
}

func TestEvaluateQuotaGuardPoliciesDryRunDoesNotApply(t *testing.T) {
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)
	policy := quotaGuardTestPolicy("dry-run")
	policy.DailyTokenLimit = 1
	policy.DryRun = true

	decision, ok := evaluateQuotaGuardPoliciesDecision(
		quotaGuardTestAccount(1, true, nil),
		[]quotaGuardConfig{policy},
		now,
		map[string]quotaGuardUsage{"Asia/Shanghai": {Daily: UpstreamUsageTotals{Tokens: 1}}},
	)

	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.True(t, decision.DryRun)
}

func TestNormalizeQuotaGuardPoliciesSupportsMultipleAccountsAndGroups(t *testing.T) {
	policies, err := normalizeQuotaGuardPolicies(quotaGuardStartRequest{
		Policies: []quotaGuardPolicyRequest{
			{ID: "cost", AccountIDs: []int64{3, 1, 3}, DailySpendLimitUSD: float64Ptr(10)},
			{ID: "tokens", AccountIDs: []int64{2}, WeeklyTokenLimit: int64Ptr(100)},
		},
	})

	require.NoError(t, err)
	require.Len(t, policies, 2)
	require.Equal(t, []int64{1, 3}, policies[0].AccountIDs)
	require.Equal(t, float64(10), policies[0].DailySpendLimitUSD)
	require.Equal(t, []int64{2}, policies[1].AccountIDs)
	require.Equal(t, int64(100), policies[1].WeeklyTokenLimit)
}

func TestQuotaGuardCalendarBoundariesUsePolicyTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, time.August, 4, 0, 30, 0, 0, location)

	require.Equal(t,
		time.Date(2026, time.August, 4, 16, 0, 0, 0, time.UTC),
		nextQuotaGuardDay(now, "Asia/Shanghai"),
	)
	require.Equal(t,
		time.Date(2026, time.August, 9, 16, 0, 0, 0, time.UTC),
		nextQuotaGuardWeek(now, "Asia/Shanghai"),
	)
}

func TestDoQuotaGuardJSONRequestPreservesHTTPErrorStatusForNonJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, status, err := doQuotaGuardJSONRequest[map[string]any](context.Background(), http.MethodGet, server.URL, "key", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, status)
}

func float64Ptr(value float64) *float64 { return &value }

func int64Ptr(value int64) *int64 { return &value }
