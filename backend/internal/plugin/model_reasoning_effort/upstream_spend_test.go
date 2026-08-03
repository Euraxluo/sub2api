package model_reasoning_effort

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func resetUpstreamSpendStateForTest() {
	upstreamSpendState.Lock()
	if upstreamSpendState.timer != nil {
		upstreamSpendState.timer.Stop()
	}
	upstreamSpendState.loaded = false
	upstreamSpendState.path = ""
	upstreamSpendState.bucket = nil
	upstreamSpendState.timer = nil
	upstreamSpendState.Unlock()
}

func TestDailyUpstreamCostTracksStandardProviderSpend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream-spend.json")
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_SPEND", path)
	resetUpstreamSpendStateForTest()
	t.Cleanup(resetUpstreamSpendStateForTest)

	RecordUpstreamUsage(42, 12.5, 100)
	RecordUpstreamUsage(42, 1.5, 25)
	RecordUpstreamCost(42, -10)
	RecordUpstreamCost(43, 4)

	require.InDelta(t, 14.0, DailyUpstreamCost(42, "Asia/Shanghai", time.Now()), 0.000001)
	require.EqualValues(t, 125, DailyUpstreamTokens(42, "Asia/Shanghai", time.Now()))
	require.InDelta(t, 4.0, DailyUpstreamCost(43, "Asia/Shanghai", time.Now()), 0.000001)
	require.Zero(t, DailyUpstreamCost(44, "Asia/Shanghai", time.Now()))

	require.NoError(t, flushUpstreamSpend())
	_, err := os.ReadFile(path)
	require.NoError(t, err)
}

func TestDailyUpstreamCostRespectsCalendarDayTimezone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream-spend.json")
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_SPEND", path)
	resetUpstreamSpendStateForTest()
	t.Cleanup(resetUpstreamSpendStateForTest)

	now := time.Date(2026, time.August, 3, 16, 30, 0, 0, time.UTC)
	upstreamSpendState.Lock()
	require.NoError(t, loadUpstreamSpendLocked())
	upstreamSpendState.bucket[upstreamSpendBucketKey(42, now.Add(-30*time.Minute))] = upstreamUsageBucket{CostUSD: 5, Tokens: 50}
	upstreamSpendState.bucket[upstreamSpendBucketKey(42, now.Add(-2*time.Hour))] = upstreamUsageBucket{CostUSD: 7, Tokens: 70}
	upstreamSpendState.bucket[upstreamSpendBucketKey(42, now.Add(-25*time.Hour))] = upstreamUsageBucket{CostUSD: 11, Tokens: 110}
	upstreamSpendState.Unlock()

	// 16:30 UTC is 00:30 in Asia/Shanghai. The 15:30 UTC bucket belongs to
	// the previous local day, while the 16:00 UTC bucket belongs to today.
	require.InDelta(t, 5.0, DailyUpstreamCost(42, "Asia/Shanghai", now), 0.000001)
	require.EqualValues(t, 50, DailyUpstreamTokens(42, "Asia/Shanghai", now))
	require.InDelta(t, 12.0, WeeklyUpstreamUsage(42, "Asia/Shanghai", now).CostUSD, 0.000001)
	require.EqualValues(t, 120, WeeklyUpstreamUsage(42, "Asia/Shanghai", now).Tokens)
}

func TestDailyUpstreamCostRetriesAfterUnreadableLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upstream-spend.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_SPEND", path)
	resetUpstreamSpendStateForTest()
	t.Cleanup(resetUpstreamSpendStateForTest)

	require.Zero(t, DailyUpstreamCost(42, "Asia/Shanghai", time.Now()))
	if err := os.WriteFile(path, []byte(`{"buckets":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	RecordUpstreamCost(42, 2)
	require.InDelta(t, 2.0, DailyUpstreamCost(42, "Asia/Shanghai", time.Now()), 0.000001)
}
