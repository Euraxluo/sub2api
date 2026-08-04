//go:build unit

package service

import (
	"testing"
)

// TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier locks in the fix
// that subscription-mode billing honours the group (and any user-specific) rate
// multiplier — i.e. cmd.SubscriptionCost tracks ActualCost (= TotalCost *
// RateMultiplier), not raw TotalCost.
func TestBuildUsageBillingCommand_SubscriptionAppliesRateMultiplier(t *testing.T) {
	t.Parallel()

	groupID := int64(7)
	subID := int64(42)

	tests := []struct {
		name           string
		totalCost      float64
		actualCost     float64
		isSubscription bool
		wantSub        float64
		wantBalance    float64
	}{
		{
			name:           "subscription with 2x multiplier consumes 2x quota",
			totalCost:      1.0,
			actualCost:     2.0,
			isSubscription: true,
			wantSub:        2.0,
			wantBalance:    0,
		},
		{
			name:           "subscription with 0.5x multiplier consumes 0.5x quota",
			totalCost:      1.0,
			actualCost:     0.5,
			isSubscription: true,
			wantSub:        0.5,
			wantBalance:    0,
		},
		{
			name:           "free subscription (multiplier 0) consumes no quota",
			totalCost:      1.0,
			actualCost:     0,
			isSubscription: true,
			wantSub:        0,
			wantBalance:    0,
		},
		{
			name:           "balance billing keeps using ActualCost (regression)",
			totalCost:      1.0,
			actualCost:     2.0,
			isSubscription: false,
			wantSub:        0,
			wantBalance:    2.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := &postUsageBillingParams{
				Cost:               &CostBreakdown{TotalCost: tt.totalCost, ActualCost: tt.actualCost},
				User:               &User{ID: 1},
				APIKey:             &APIKey{ID: 2, GroupID: &groupID},
				Account:            &Account{ID: 3},
				Subscription:       &UserSubscription{ID: subID},
				IsSubscriptionBill: tt.isSubscription,
			}

			cmd := buildUsageBillingCommand("req-1", nil, p)
			if cmd == nil {
				t.Fatal("buildUsageBillingCommand returned nil")
			}
			if cmd.SubscriptionCost != tt.wantSub {
				t.Errorf("SubscriptionCost = %v, want %v", cmd.SubscriptionCost, tt.wantSub)
			}
			if cmd.BalanceCost != tt.wantBalance {
				t.Errorf("BalanceCost = %v, want %v", cmd.BalanceCost, tt.wantBalance)
			}
		})
	}
}

func TestBuildUsageBillingCommand_AccountQuotaUsesAccountStatsCost(t *testing.T) {
	groupID := int64(7)
	accountRate := 0.5
	accountStatsCost := 3.0

	p := &postUsageBillingParams{
		Cost:                  &CostBreakdown{TotalCost: 10, ActualCost: 20},
		User:                  &User{ID: 1},
		APIKey:                &APIKey{ID: 2, GroupID: &groupID},
		Account:               &Account{ID: 3, Type: AccountTypeAPIKey, Extra: map[string]any{"quota_limit": 100}},
		AccountRateMultiplier: accountRate,
		AccountStatsCost:      &accountStatsCost,
	}

	cmd := buildUsageBillingCommand("req-account-stats", nil, p)
	if cmd == nil {
		t.Fatal("buildUsageBillingCommand returned nil")
	}
	if diff := cmd.AccountQuotaCost - accountStatsCost*accountRate; diff < -1e-12 || diff > 1e-12 {
		t.Errorf("AccountQuotaCost = %v, want %v", cmd.AccountQuotaCost, accountStatsCost*accountRate)
	}
}

func TestBuildUsageBillingCommand_UsesStrategyUserChargeInsteadOfActualCost(t *testing.T) {
	groupID := int64(7)
	strategyCharge := 7.5

	p := &postUsageBillingParams{
		Cost:           &CostBreakdown{TotalCost: 3, ActualCost: 1},
		UserChargeCost: &strategyCharge,
		User:           &User{ID: 1},
		APIKey:         &APIKey{ID: 2, GroupID: &groupID},
		Account:        &Account{ID: 3},
	}

	cmd := buildUsageBillingCommand("req-strategy-user-charge", nil, p)
	if cmd == nil {
		t.Fatal("buildUsageBillingCommand returned nil")
	}
	if cmd.BalanceCost != strategyCharge {
		t.Errorf("BalanceCost = %v, want %v", cmd.BalanceCost, strategyCharge)
	}
}

func TestBuildUsageBillingCommand_StrategyChargeKeepsSubscriptionBillingType(t *testing.T) {
	groupID := int64(7)
	subscriptionID := int64(8)
	strategyCharge := 2.5

	p := &postUsageBillingParams{
		Cost:               &CostBreakdown{},
		UserChargeCost:     &strategyCharge,
		User:               &User{ID: 1},
		APIKey:             &APIKey{ID: 2, GroupID: &groupID},
		Account:            &Account{ID: 3},
		Subscription:       &UserSubscription{ID: subscriptionID},
		IsSubscriptionBill: true,
	}

	cmd := buildUsageBillingCommand("req-strategy-subscription", nil, p)
	if cmd == nil {
		t.Fatal("buildUsageBillingCommand returned nil")
	}
	if cmd.SubscriptionCost != strategyCharge {
		t.Errorf("SubscriptionCost = %v, want %v", cmd.SubscriptionCost, strategyCharge)
	}
	if cmd.BalanceCost != 0 {
		t.Errorf("BalanceCost = %v, want 0", cmd.BalanceCost)
	}
}
