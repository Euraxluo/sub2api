package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	billingstrategy "github.com/Wei-Shaw/sub2api/internal/plugin/billing_strategy"
	"github.com/stretchr/testify/require"
)

func TestResolveMaxCostBillingDecisionBuildsVirtualCostWithoutDoubleApplyingAccountRate(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/accounts/42", request.URL.Path)
		require.Equal(t, "admin-test", request.Header.Get("x-api-key"))
		_, _ = response.Write([]byte(`{"code":0,"message":"success","data":{"id":42,"rate_multiplier":0.04}}`))
	}))
	defer server.Close()
	t.Setenv("SUB2API_BILLING_STRATEGY_BASE_URL", server.URL)
	t.Setenv("SUB2API_BILLING_STRATEGY_ADMIN_API_KEY", "admin-test")
	_, err := billingstrategy.SetAccountRateMultiplierFactor([]int64{42}, 20)
	require.NoError(t, err)

	decision := resolveMaxCostBillingDecision(
		42,
		"",
		"",
		[]string{"winner", "cheaper"},
		func(model string) (*CostBreakdown, error) {
			if model == "winner" {
				return &CostBreakdown{BillingMode: "token", InputCost: 0.05, OutputCost: 0.018752, TotalCost: 0.068752}, nil
			}
			return &CostBreakdown{BillingMode: "token", InputCost: 0.00275, TotalCost: 0.00275}, nil
		},
		nil,
		context.Background(),
	)
	require.NoError(t, decision.AccountSnapshotError)
	require.NotNil(t, decision)
	require.NotNil(t, decision.RawCost)
	require.NotNil(t, decision.SelectedCost)
	require.InDelta(t, 0.8, decision.AccountRateMultiplier, 1e-12)
	require.InDelta(t, 0.068752, decision.RawCost.TotalCost, 1e-12)
	require.InDelta(t, 0.0550016, decision.SelectedCost.TotalCost, 1e-12)
	require.InDelta(t, 0.04, decision.SelectedCost.InputCost, 1e-12)
	require.InDelta(t, 0.0150016, decision.SelectedCost.OutputCost, 1e-12)

	userCharge := decision.SelectedCost.TotalCost * 1.5
	accountStatsCost := decision.RawCost.TotalCost
	groupID := int64(7)
	command := buildUsageBillingCommand("req-max-cost", nil, &postUsageBillingParams{
		Cost:                  &CostBreakdown{TotalCost: accountStatsCost, ActualCost: userCharge},
		UserChargeCost:        &userCharge,
		User:                  &User{ID: 1},
		APIKey:                &APIKey{ID: 2, GroupID: &groupID},
		Account:               &Account{ID: 42, Type: AccountTypeAPIKey, Extra: map[string]any{"quota_limit": 100}},
		AccountRateMultiplier: decision.AccountRateMultiplier,
		AccountStatsCost:      &accountStatsCost,
	})
	require.NotNil(t, command)
	require.InDelta(t, 0.0825024, command.BalanceCost, 1e-12)
	require.InDelta(t, 0.0550016, command.AccountQuotaCost, 1e-12)
}
