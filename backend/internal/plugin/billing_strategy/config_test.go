package billing_strategy

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountRateMultiplierFactorDefaultsToOneAndPersistsOverrides(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	require.Equal(t, 1.0, AccountRateMultiplierFactor(42))
	config, err := SetAccountRateMultiplierFactor([]int64{42, 43}, 20)
	require.NoError(t, err)
	require.Equal(t, 20.0, config.AccountMultipliers["42"])
	require.Equal(t, 20.0, AccountRateMultiplierFactor(42))

	config, err = SetAccountRateMultiplierFactor([]int64{42}, 1)
	require.NoError(t, err)
	require.NotContains(t, config.AccountMultipliers, "42")
	require.Equal(t, 1.0, AccountRateMultiplierFactor(42))
}

func TestSetAccountRateMultiplierFactorRejectsInvalidValues(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	for _, factor := range []float64{-1, 0, maxAccountMultiplier + 1} {
		_, err := SetAccountRateMultiplierFactor([]int64{42}, factor)
		require.ErrorIs(t, err, ErrInvalidAccountMultiplier)
	}
	_, err := SetAccountRateMultiplierFactor([]int64{0}, 20)
	require.ErrorIs(t, err, ErrInvalidAccountMultiplier)
}

func TestApplyAccountSnapshotUsesWinningRawCost(t *testing.T) {
	t.Setenv("SUB2API_BILLING_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	_, err := SetAccountRateMultiplierFactor([]int64{42}, 20)
	require.NoError(t, err)

	decision := applyAccountSnapshot(Decision{
		SelectedModel: "gpt-5.6-sol",
		Candidates: []Candidate{
			{Model: "gpt-5.6-sol", Available: true, TotalCost: 0.068752},
			{Model: "gpt-5.6-luna", Available: true, TotalCost: 0.00275},
		},
	}, AccountSnapshot{AccountID: 42, RateMultiplier: 0.04})

	require.True(t, decision.AccountSnapshotResolved)
	require.InDelta(t, 0.04, decision.AccountBaseRateMultiplier, 1e-12)
	require.InDelta(t, 20, decision.AccountRateMultiplierFactor, 1e-12)
	require.InDelta(t, 0.8, decision.AccountRateMultiplier, 1e-12)
	require.InDelta(t, 0.0550016, decision.VirtualModelCost, 1e-12)
	require.InDelta(t, 0.0550016, *decision.Candidates[0].AccountBilledCost, 1e-12)
	require.InDelta(t, 0.0022, *decision.Candidates[1].AccountBilledCost, 1e-12)

	virtualCost := CalculateVirtualModelCost(42, 0.068752, 0.04)
	require.InDelta(t, 0.0550016, virtualCost, 1e-12)
	require.InDelta(t, 0.0825024, CalculateUserChargeCost(virtualCost, 1.5), 1e-12)
}
