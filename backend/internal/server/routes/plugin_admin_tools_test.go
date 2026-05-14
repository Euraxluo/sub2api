package routes

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEvaluateCodexQuotaGuardDecisionBlocksNearExhaustion(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	account := codexGuardAccount{
		ID:          1,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_used_percent": 99.2,
			"codex_5h_reset_at":     now.Add(30 * time.Minute).Format(time.RFC3339),
		},
	}
	cfg := codexQuotaGuardConfig{
		Enabled:         true,
		ReservePercent:  1,
		Windows:         []string{"5h", "7d"},
		IntervalSeconds: 60,
		AccountTypes:    []string{"oauth"},
	}

	decision, ok := evaluateCodexQuotaGuardDecision(account, cfg, now)
	require.True(t, ok)
	require.Equal(t, "block", decision.Action)
	require.Equal(t, true, decision.Extra["codex_quota_guard_managed"])
	require.Equal(t, "5h", decision.Extra["codex_quota_guard_blocked_window"])
}

func TestEvaluateCodexQuotaGuardDecisionSkipsExpiredWindow(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	account := codexGuardAccount{
		ID:          2,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Schedulable: true,
		Extra: map[string]any{
			"codex_5h_used_percent": 99.9,
			"codex_5h_reset_at":     now.Add(-1 * time.Minute).Format(time.RFC3339),
		},
	}
	cfg := codexQuotaGuardConfig{
		Enabled:         true,
		ReservePercent:  1,
		Windows:         []string{"5h"},
		IntervalSeconds: 60,
		AccountTypes:    []string{"oauth"},
	}

	_, ok := evaluateCodexQuotaGuardDecision(account, cfg, now)
	require.False(t, ok)
}

func TestEvaluateCodexQuotaGuardDecisionReleasesManagedAccount(t *testing.T) {
	now := time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	account := codexGuardAccount{
		ID:          3,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Schedulable: false,
		Extra: map[string]any{
			"codex_quota_guard_managed":       true,
			"codex_quota_guard_blocked_until": now.Add(-1 * time.Minute).Format(time.RFC3339),
		},
	}
	cfg := codexQuotaGuardConfig{
		Enabled:         true,
		ReservePercent:  1,
		Windows:         []string{"5h"},
		IntervalSeconds: 60,
		AccountTypes:    []string{"oauth"},
	}

	decision, ok := evaluateCodexQuotaGuardDecision(account, cfg, now)
	require.True(t, ok)
	require.Equal(t, "release", decision.Action)
	require.Equal(t, false, decision.Extra["codex_quota_guard_managed"])
}
