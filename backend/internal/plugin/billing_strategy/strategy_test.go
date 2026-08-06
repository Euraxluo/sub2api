package billing_strategy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveMaxCostDecisionBuildsAccountAdjustedVirtualModelCost(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		require.Equal(t, "/api/v1/admin/accounts/42", request.URL.Path)
		require.Equal(t, "admin-test", request.Header.Get("x-api-key"))
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"message":"success","data":{"id":42,"rate_multiplier":0.04}}`))
	}))
	defer server.Close()
	t.Setenv(accountSnapshotBaseURLEnv, server.URL)
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "admin-test")
	_, err := SetAccountRateMultiplierFactor([]int64{42}, 20)
	require.NoError(t, err)

	decision, err := ResolveMaxCostDecisionWithAccountSnapshot(context.Background(), DecisionInput{
		AccountID: 42,
		Models:    []string{"winner", "cheaper"},
		Evaluate: func(model string) (Candidate, error) {
			cost := 0.00275
			if model == "winner" {
				cost = 0.068752
			}
			return Candidate{Available: true, TotalCost: cost}, nil
		},
	})
	require.NoError(t, err)

	require.Equal(t, int32(1), requests.Load())
	require.Equal(t, "winner", decision.SelectedModel)
	require.InDelta(t, 0.068752, decision.Candidates[0].TotalCost, 1e-12)
	require.InDelta(t, 0.04, decision.AccountBaseRateMultiplier, 1e-12)
	require.InDelta(t, 20, decision.AccountRateMultiplierFactor, 1e-12)
	require.InDelta(t, 0.8, decision.AccountRateMultiplier, 1e-12)
	require.InDelta(t, 0.0550016, decision.VirtualModelCost, 1e-12)
	require.InDelta(t, 0.0825024, CalculateUserChargeCost(decision.VirtualModelCost, 1.5), 1e-12)
}

func TestResolveMaxCostDecisionFailsWhenAccountSnapshotRequestFails(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv(accountSnapshotBaseURLEnv, server.URL)
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "invalid")

	decision, err := ResolveMaxCostDecisionWithAccountSnapshot(context.Background(), DecisionInput{
		AccountID: 42,
		Models:    []string{"winner"},
		Evaluate: func(string) (Candidate, error) {
			return Candidate{Available: true, TotalCost: 2}, nil
		},
	})

	require.ErrorContains(t, err, "HTTP 401")
	require.Equal(t, "winner", decision.SelectedModel)
	require.False(t, decision.AccountSnapshotResolved)
	require.Zero(t, decision.VirtualModelCost)
}

func TestResolveMaxCostDecisionFallsBackToSelectedAccountRate(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(accountSnapshotBaseURLEnv, "")
	t.Setenv(accountSnapshotAdminAPIKeyEnv, "")

	ctx := WithAccountRateMultiplier(context.Background(), 42, 0.08)
	decision, err := ResolveMaxCostDecisionWithAccountSnapshot(ctx, DecisionInput{
		AccountID: 42,
		Models:    []string{"winner"},
		Evaluate: func(string) (Candidate, error) {
			return Candidate{Available: true, TotalCost: 0.109867}, nil
		},
	})

	require.NoError(t, err)
	require.Equal(t, "winner", decision.SelectedModel)
	require.True(t, decision.AccountSnapshotResolved)
	require.InDelta(t, 0.08, decision.AccountBaseRateMultiplier, 1e-12)
	require.InDelta(t, 0.08, decision.AccountRateMultiplier, 1e-12)
	require.InDelta(t, 0.00878936, decision.VirtualModelCost, 1e-12)
}

func TestResolveMaxCostDecisionKeepsSelectionUnmultiplied(t *testing.T) {
	decision := ResolveMaxCostDecision(DecisionInput{
		Models: []string{"cheap", "cheap", "expensive"},
		PricingSource: func(model string) string {
			return "channel:" + model
		},
		Evaluate: func(model string) (Candidate, error) {
			cost := 1.0
			if model == "expensive" {
				cost = 2
			}
			return Candidate{Available: true, TotalCost: cost}, nil
		},
	})

	require.Equal(t, "expensive", decision.SelectedModel)
	require.Len(t, decision.Candidates, 2)
	require.Equal(t, "channel:cheap", decision.Candidates[0].PricingSource)
	require.Equal(t, 2.0, decision.Candidates[1].TotalCost)
}

func TestBillingModelSourceOverrideRejectsUnknownValues(t *testing.T) {
	extra := map[string]any{AccountBillingModelSourceExtraKey: " MAX_COST "}
	require.Equal(t, BillingModelSourceMaxCost, AccountBillingModelSourceOverride(extra))
	require.Equal(t, BillingModelSourceMaxCost, EffectiveBillingModelSource(extra, BillingModelSourceUpstream))

	extra[AccountBillingModelSourceExtraKey] = "unknown"
	require.Empty(t, AccountBillingModelSourceOverride(extra))
	require.Equal(t, BillingModelSourceUpstream, EffectiveBillingModelSource(extra, BillingModelSourceUpstream))
}

func TestBillingRateAndChargeUseSelectedRawCost(t *testing.T) {
	require.Equal(t, 3.0, CalculateUserChargeCost(2, 1.5))
	require.Equal(t, 7.0, BillingRate("token", 7, 3, 5, 2, 0, 0, 0))
	require.Equal(t, 3.0, BillingRate("image", 7, 3, 5, 2, 1, 0, 0))
	require.Equal(t, 5.0, BillingRate("per_request", 7, 3, 5, 2, 0, 1, 0))
	require.Equal(t, 2.0, BillingRate("per_request", 7, 3, 5, 2, 0, 0, 1))
}

func TestCalculateAccountUserChargeCostUsesResolvedAccountMultiplier(t *testing.T) {
	const rawTokenCost = 0.109867
	const accountBaseMultiplier = 0.08
	const accountMultiplier = 20.0

	require.InDelta(t, 0.1757872,
		CalculateAccountUserChargeCost(rawTokenCost, accountBaseMultiplier*accountMultiplier), 1e-12)
}
