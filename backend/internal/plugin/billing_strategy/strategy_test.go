package billing_strategy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
