package model_reasoning_effort

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTransformBodyModelReasoningEffort(t *testing.T) {
	mappings := []Mapping{
		{From: "gpt-5", To: "gpt-5.2", Effort: "medium"},
		{From: "o3-*", Effort: "low"},
	}

	responses := TransformBody([]byte(`{"model":"gpt-5.2","input":[]}`), mappings)
	require.Equal(t, "medium", gjson.GetBytes(responses, "reasoning.effort").String())
	remapped := TransformBody([]byte(`{"model":"gpt-5","input":[]}`), mappings)
	require.Equal(t, "gpt-5.2", gjson.GetBytes(remapped, "model").String())
	require.Equal(t, "medium", gjson.GetBytes(remapped, "reasoning.effort").String())

	chat := TransformBody([]byte(`{"model":"o3-mini","messages":[]}`), mappings)
	require.Equal(t, "low", gjson.GetBytes(chat, "reasoning_effort").String())

	unchanged := TransformBody([]byte(`{"model":"gpt-4","input":[]}`), mappings)
	require.JSONEq(t, `{"model":"gpt-4","input":[]}`, string(unchanged))
}

func TestTransformBodyMatchesSourceModelAndEffort(t *testing.T) {
	mappings := []Mapping{
		{FromModel: "gpt-5.6-sol", FromEffort: "default", ToModel: "gpt-5.6-luna", ToEffort: "default"},
		{FromModel: "gpt-5.6-sol", FromEffort: "xhigh", ToModel: "gpt-5.5", ToEffort: "xhigh"},
		{FromModel: "gpt-5.6-sol", FromEffort: "max", ToModel: "gpt-5.6-luna", ToEffort: "max"},
	}

	xhigh := TransformBody([]byte(`{"model":"gpt-5.6-sol","reasoning_effort":"xhigh","messages":[]}`), mappings)
	require.Equal(t, "gpt-5.5", gjson.GetBytes(xhigh, "model").String())
	require.Equal(t, "xhigh", gjson.GetBytes(xhigh, "reasoning_effort").String())

	max := TransformBody([]byte(`{"model":"gpt-5.6-sol","reasoning_effort":"max","messages":[]}`), mappings)
	require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(max, "model").String())
	require.Equal(t, "max", gjson.GetBytes(max, "reasoning_effort").String())

	defaultEffort := TransformBody([]byte(`{"model":"gpt-5.6-sol","messages":[]}`), mappings)
	require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(defaultEffort, "model").String())
	require.Equal(t, "medium", gjson.GetBytes(defaultEffort, "reasoning_effort").String())

	responses := TransformBody([]byte(`{"model":"gpt-5.6-sol","input":[]}`), mappings)
	require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(responses, "model").String())
	require.Equal(t, "medium", gjson.GetBytes(responses, "reasoning.effort").String())
}

func TestDefaultEffortIsCanonicalizedToMedium(t *testing.T) {
	normalized, err := NormalizeConfig(Config{Accounts: map[string]AccountConfig{
		"42": {Mappings: []Mapping{{
			FromModel:  "gpt-5.6-sol",
			FromEffort: "default",
			ToModel:    "gpt-5.6-luna",
			ToEffort:   "default",
		}}},
	}})
	require.NoError(t, err)
	require.Equal(t, "medium", normalized.Accounts["42"].Mappings[0].FromEffort)
	require.Equal(t, "medium", normalized.Accounts["42"].Mappings[0].ToEffort)

	noEffort := TransformBody([]byte(`{"model":"gpt-5.6-sol","messages":[]}`), normalized.Accounts["42"].Mappings)
	require.Equal(t, "medium", gjson.GetBytes(noEffort, "reasoning_effort").String())
}

func TestMergeRadarMetricsKeepsGPTOnlyAndReadsTaskCosts(t *testing.T) {
	history := []byte(`{
		"gpt-5.6-luna@max":[{"ts":"2026-08-03T00:00:00Z","score":91.1,"n":10}],
		"gpt-5.6-sol@xhigh":[{"ts":"2026-08-03T00:00:00Z","score":99.1,"n":10}],
		"deepseek-v4@max":[{"ts":"2026-08-03T00:00:00Z","score":110,"n":10}]
	}`)
	table := []byte(`{"cells":{
		"task-a|gpt-5.6-luna|max":{"cost":0.4,"total_n":2},
		"task-b|gpt-5.6-luna|max":{"cost":0.6,"total_n":1},
		"task-a|gpt-5.6-sol|xhigh":{"cost":3.0,"total_n":1}
	}}`)
	metrics := mergeRadarMetrics(history, table, AutoRoutingConfig{IQAggregation: "max"})
	require.Len(t, metrics, 2)
	byKey := make(map[string]ModelMetric, len(metrics))
	for _, metric := range metrics {
		byKey[metric.Model+"@"+metric.Effort] = metric
	}
	require.InDelta(t, 0.4666, byKey["gpt-5.6-luna@max"].CostUSD, 0.001)
	require.Equal(t, 3.0, byKey["gpt-5.6-sol@xhigh"].CostUSD)
}

func TestRadarLatestSeriesNewestPointDrivesAutomaticMappings(t *testing.T) {
	history := []byte(`{
		"gpt-5.6-luna@max":[{"ts":"2026-08-05T01:00:00Z","score":96.0}],
		"latest:gpt-5.6-luna":[{"ts":"2026-08-05T01:00:00Z","score":61.3}],
		"latest:gpt-5.6-luna@medium":[{"ts":"2026-08-05T01:00:00Z","score":38.8}],
		"latest:gpt-5.6-luna@max":[
			{"ts":"2026-08-05T01:00:00Z","score":97.8},
			{"ts":"2026-08-03T21:00:00Z","score":105.8}
		],
		"latest:gpt-5.6-sol@ultra":[{"ts":"2026-08-05T01:00:00Z","score":100.4}],
		"latest:gpt-5.6-sol@max":[{"ts":"2026-08-05T01:00:00Z","score":103.1}],
		"latest:gpt-5.6-sol@xhigh":[{"ts":"2026-08-05T01:00:00Z","score":107.1}],
		"latest:gpt-5.6-terra@ultra":[{"ts":"2026-08-05T01:00:00Z","score":105.8}]
	}`)
	table := []byte(`{"cells":{
		"task|gpt-5.6-luna|max":{"cost":0.46,"total_n":1},
		"task|gpt-5.6-sol|ultra":{"cost":22.50,"total_n":1},
		"task|gpt-5.6-sol|max":{"cost":9.33,"total_n":1},
		"task|gpt-5.6-sol|xhigh":{"cost":6.38,"total_n":1},
		"task|gpt-5.6-terra|ultra":{"cost":9.87,"total_n":1}
	}}`)
	metrics := mergeRadarMetrics(history, table, AutoRoutingConfig{
		IQAggregation: "max",
		IQWindowHours: 168,
	})
	byMetric := make(map[string]ModelMetric, len(metrics))
	for _, metric := range metrics {
		byMetric[metric.Model+"@"+metric.Effort] = metric
	}
	require.InDelta(t, 97.8, byMetric["gpt-5.6-luna@max"].IQ, 0.001)
	require.InDelta(t, 38.8, byMetric["gpt-5.6-luna@medium"].IQ, 0.001)
	require.True(t, byMetric["gpt-5.6-luna@max"].HasIQ)

	plan, err := ComputeAutomaticMappings(metrics, AutoMappingOptions{
		BaselineModel: "gpt-5.6-luna",
		BaselineGap:   5,
		FormulaMode:   "natural_gap",
		GPTOnly:       true,
	})
	require.NoError(t, err)
	require.InDelta(t, 97.8, plan.Baseline.IQ, 0.001)
	require.InDelta(t, 102.8, plan.BaselineLimit, 0.001)
	require.InDelta(t, 107.1, plan.MaxIQ, 0.001)

	bySource := make(map[string]Mapping, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		bySource[mapping.FromModel+"@"+mapping.FromEffort] = mapping
	}
	require.Equal(t, "gpt-5.6-luna@max", bySource["gpt-5.6-sol@ultra"].ToModel+"@"+bySource["gpt-5.6-sol@ultra"].ToEffort)
	require.Equal(t, "gpt-5.6-sol@xhigh", bySource["gpt-5.6-sol@max"].ToModel+"@"+bySource["gpt-5.6-sol@max"].ToEffort)
	require.Equal(t, "gpt-5.6-sol@xhigh", bySource["gpt-5.6-terra@ultra"].ToModel+"@"+bySource["gpt-5.6-terra@ultra"].ToEffort)
	require.InDelta(t, 103.1, plan.Bands[1].MinIQ, 0.001)
	require.Zero(t, plan.Bands[0].MaxIQ)
	require.Zero(t, plan.Bands[1].MaxIQ)
	require.InDelta(t, 105.8, plan.Bands[2].MinIQ, 0.001)
	require.Zero(t, plan.Bands[2].MaxIQ)
}

func TestRadarRefreshEndpointPreservesQueryAndAddsCacheBuster(t *testing.T) {
	endpoint := radarRefreshEndpoint("https://api.codexradar.com/api/v1/iq-history?existing=1", "snapshot-42")
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Equal(t, "1", parsed.Query().Get("existing"))
	require.Equal(t, "snapshot-42", parsed.Query().Get("sub2api_refresh"))
}

func TestFetchRadarJSONRequestsFreshSnapshot(t *testing.T) {
	var cacheControl string
	var pragma string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cacheControl = request.Header.Get("Cache-Control")
		pragma = request.Header.Get("Pragma")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	body, err := fetchRadarJSON(context.Background(), server.Client(), server.URL, 1024)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
	require.Equal(t, "no-cache", cacheControl)
	require.Equal(t, "no-cache", pragma)
}

func TestMappingStatsSeparateSourceEfforts(t *testing.T) {
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_STATS", t.TempDir()+"/stats.json")
	require.NoError(t, SaveConfig(Config{Accounts: map[string]AccountConfig{
		"42": {Mappings: []Mapping{
			{FromModel: "gpt-5.6-sol", FromEffort: "xhigh", ToModel: "gpt-5.5", ToEffort: "xhigh"},
			{FromModel: "gpt-5.6-sol", FromEffort: "max", ToModel: "gpt-5.6-luna", ToEffort: "max"},
		}},
	}}))
	RecordUsageMapping(42, "gpt-5.6-sol", "xhigh", 10, 2)
	RecordUsageMapping(42, "gpt-5.6-sol", "max", 20, 3)
	stats, err := LoadMappingUsageStats()
	require.NoError(t, err)
	require.Len(t, stats, 2)
	byEffort := make(map[string]MappingUsageStat, len(stats))
	for _, stat := range stats {
		byEffort[stat.FromEffort] = stat
	}
	require.Equal(t, int64(1), byEffort["xhigh"].Requests)
	require.Equal(t, int64(20), byEffort["max"].InputTokens)
}

func TestComputeAutomaticMappingsUsesLunaBaselineAndNaturalGap(t *testing.T) {
	plan, err := ComputeAutomaticMappings([]ModelMetric{
		{Model: "gpt-5.6-luna", Effort: "default", IQ: 54.1, HasCost: true, CostUSD: 0.02},
		{Model: "gpt-5.6-luna", Effort: "max", IQ: 91.1, HasCost: true, CostUSD: 0.45},
		{Model: "gpt-5.6-near", Effort: "high", IQ: 94.0, HasCost: true, CostUSD: 0.10},
		{Model: "gpt-5.6-sol", Effort: "xhigh", IQ: 97.8, HasCost: true, CostUSD: 3.0},
		{Model: "gpt-5.6-terra", Effort: "ultra", IQ: 99.1, HasCost: true, CostUSD: 2.0},
		{Model: "gpt-5.5", Effort: "xhigh", IQ: 100.4, HasCost: true, CostUSD: 1.0},
		{Model: "gpt-5.6-sol", Effort: "max", IQ: 105.8, HasCost: true, CostUSD: 9.0},
		{Model: "gpt-5.6-sol", Effort: "ultra", IQ: 105.8, HasCost: true, CostUSD: 20.0},
		{Model: "deepseek-v4", Effort: "max", IQ: 110, HasCost: true, CostUSD: 0.01},
	}, AutoMappingOptions{BaselineModel: "gpt-5.6-luna", BaselineGap: 5, GPTOnly: true})
	require.NoError(t, err)
	require.Equal(t, 91.1, plan.Baseline.IQ)
	require.Equal(t, 96.1, plan.BaselineLimit)
	require.Len(t, plan.Bands, 3)

	bySource := make(map[string]Mapping, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		bySource[mapping.FromModel+"@"+mapping.FromEffort] = mapping
	}
	require.Equal(t, "gpt-5.6-luna@max", bySource["gpt-5.6-luna@medium"].ToModel+"@"+bySource["gpt-5.6-luna@medium"].ToEffort)
	require.Equal(t, "gpt-5.6-luna@max", bySource["gpt-5.6-near@high"].ToModel+"@"+bySource["gpt-5.6-near@high"].ToEffort)
	require.Equal(t, "gpt-5.5@xhigh", bySource["gpt-5.6-sol@xhigh"].ToModel+"@"+bySource["gpt-5.6-sol@xhigh"].ToEffort)
	require.Equal(t, "gpt-5.6-sol@max", bySource["gpt-5.6-sol@ultra"].ToModel+"@"+bySource["gpt-5.6-sol@ultra"].ToEffort)
}

func TestComputeAutomaticMappingsIQCostFormulaUsesRadarCostAndIQ(t *testing.T) {
	plan, err := ComputeAutomaticMappings([]ModelMetric{
		{Model: "gpt-5.6-luna", Effort: "max", IQ: 90, HasIQ: true, HasCost: true, CostUSD: 0.50},
		{Model: "gpt-5.6-a", Effort: "high", IQ: 97, HasIQ: true, HasCost: true, CostUSD: 1.00},
		{Model: "gpt-5.6-b", Effort: "high", IQ: 101, HasIQ: true, HasCost: true, CostUSD: 1.03},
		{Model: "gpt-5.6-c", Effort: "max", IQ: 140, HasIQ: true, HasCost: true, CostUSD: 10.00},
	}, AutoMappingOptions{
		BaselineModel: "gpt-5.6-luna",
		BaselineGap:   5,
		FormulaMode:   "iq_cost",
		GPTOnly:       true,
	})
	require.NoError(t, err)

	bySource := make(map[string]Mapping, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		bySource[mapping.FromModel+"@"+mapping.FromEffort] = mapping
	}
	require.Equal(t, "gpt-5.6-b@high", bySource["gpt-5.6-a@high"].ToModel+"@"+bySource["gpt-5.6-a@high"].ToEffort)
	require.Equal(t, "gpt-5.6-b@high", bySource["gpt-5.6-b@high"].ToModel+"@"+bySource["gpt-5.6-b@high"].ToEffort)
	fallback := TransformBody([]byte(`{"model":"gpt-5.4","input":[]}`), plan.Mappings)
	require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(fallback, "model").String())
	require.Equal(t, "max", gjson.GetBytes(fallback, "reasoning.effort").String())
}

func TestAutomaticMappingsUseHighestLunaAndCoverUnobservedGPTModels(t *testing.T) {
	plan, err := ComputeAutomaticMappings([]ModelMetric{
		{Model: "gpt-5.6-luna", Effort: "max", IQ: 91.1, HasIQ: true, HasCost: true, CostUSD: 0.45},
		{Model: "gpt-5.7-luna", Effort: "high", IQ: 94.2, HasIQ: true, HasCost: true, CostUSD: 0.80},
		{Model: "gpt-5.6-sol", Effort: "xhigh", IQ: 100.4, HasIQ: true, HasCost: true, CostUSD: 1.0},
		{Model: "gpt-5.4", Effort: "medium"},
		{Model: "gpt-5", Effort: "high"},
		{Model: "gpt-5.6", Effort: "medium"},
		{Model: "deepseek-v4", Effort: "max", IQ: 110, HasIQ: true, HasCost: true, CostUSD: 0.01},
	}, AutoMappingOptions{BaselineModel: "gpt-5.6-luna", BaselineGap: 5, GPTOnly: true})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.7-luna", plan.Baseline.Model)
	require.Equal(t, "high", plan.Baseline.Effort)

	bySource := make(map[string]Mapping, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		bySource[mapping.FromModel+"@"+mapping.FromEffort] = mapping
	}
	require.Equal(t, "gpt-5.7-luna@high", bySource["gpt-5.4@medium"].ToModel+"@"+bySource["gpt-5.4@medium"].ToEffort)
	require.Equal(t, "gpt-5.7-luna@high", bySource["gpt-5@high"].ToModel+"@"+bySource["gpt-5@high"].ToEffort)

	unknownEffort := TransformBody([]byte(`{"model":"gpt-5.4","reasoning_effort":"xhigh","input":[]}`), plan.Mappings)
	require.Equal(t, "gpt-5.7-luna", gjson.GetBytes(unknownEffort, "model").String())
	require.Equal(t, "high", gjson.GetBytes(unknownEffort, "reasoning_effort").String())
	unknownModel := TransformBody([]byte(`{"model":"gpt-5.6","messages":[]}`), plan.Mappings)
	require.Equal(t, "gpt-5.7-luna", gjson.GetBytes(unknownModel, "model").String())
	require.Equal(t, "high", gjson.GetBytes(unknownModel, "reasoning_effort").String())

	for _, model := range []string{"gpt-5", "gpt-5.4", "gpt-5.6", "gpt-5.6-sol", "gpt-4.1-mini"} {
		require.Truef(t, isGPTModel(model, true), "expected GPT model: %s", model)
	}
	for _, model := range []string{"gpt-image-1", "gpt-5-preview", "claude-3.7", "deepseek-v4"} {
		require.Falsef(t, isGPTModel(model, true), "expected non-GPT model: %s", model)
	}
}

func TestAutomaticCoverageUsesConfiguredLunaBeforeFirstRadarRefresh(t *testing.T) {
	previous := autoSnapshot.Load()
	autoSnapshot.Store(nil)
	t.Cleanup(func() { autoSnapshot.Store(previous) })

	mappings := accountMappings(Config{Auto: AutoRoutingConfig{
		Enabled:       true,
		BaselineModel: "gpt-5.7-luna",
	}}, 42)
	require.Len(t, mappings, 1)

	body := TransformBody([]byte(`{"model":"gpt-5.4","input":[]}`), mappings)
	require.Equal(t, "gpt-5.7-luna", gjson.GetBytes(body, "model").String())
	require.Equal(t, "max", gjson.GetBytes(body, "reasoning.effort").String())
}

func TestAutomaticMappingCanBeScopedToSelectedAccounts(t *testing.T) {
	previous := autoSnapshot.Load()
	autoSnapshot.Store(nil)
	t.Cleanup(func() { autoSnapshot.Store(previous) })

	config := Config{
		Auto: AutoRoutingConfig{
			Enabled:    true,
			AccountIDs: []int64{42},
		},
		Accounts: map[string]AccountConfig{
			"43": {Mappings: []Mapping{{FromModel: "gpt-5.4", FromEffort: "medium", ToModel: "gpt-5.5", ToEffort: "low"}}},
		},
	}

	require.Len(t, accountMappings(config, 42), 1)
	selectedBody := TransformBody([]byte(`{"model":"gpt-5.4","input":[]}`), accountMappings(config, 42))
	require.Equal(t, "gpt-5.6-luna", gjson.GetBytes(selectedBody, "model").String())

	unselectedMappings := accountMappings(config, 43)
	require.Len(t, unselectedMappings, 1)
	unselectedBody := TransformBody([]byte(`{"model":"gpt-5.4","input":[]}`), unselectedMappings)
	require.Equal(t, "gpt-5.5", gjson.GetBytes(unselectedBody, "model").String())
	require.Equal(t, "low", gjson.GetBytes(unselectedBody, "reasoning.effort").String())
}

func TestUnavailableModelsSkipAutoAndManualMappings(t *testing.T) {
	previous := autoSnapshot.Load()
	autoSnapshot.Store(nil)
	t.Cleanup(func() { autoSnapshot.Store(previous) })

	config := Config{
		Auto: AutoRoutingConfig{
			Enabled:           true,
			BaselineModel:     "gpt-5.6-luna",
			UnavailableModels: []string{"GPT-5.6-LUNA"},
		},
		Accounts: map[string]AccountConfig{
			"42": {Mappings: []Mapping{
				{FromModel: "gpt-5.6-sol", FromEffort: "max", ToModel: "gpt-5.6-luna", ToEffort: "max"},
				{FromModel: "gpt-5.6-sol", FromEffort: "xhigh", ToModel: "gpt-5.5", ToEffort: "xhigh"},
			}},
		},
	}

	maxBody := TransformBody([]byte(`{"model":"gpt-5.6-sol","reasoning_effort":"max","messages":[]}`), accountMappings(config, 42))
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(maxBody, "model").String())

	xhighBody := TransformBody([]byte(`{"model":"gpt-5.6-sol","reasoning_effort":"xhigh","messages":[]}`), accountMappings(config, 42))
	require.Equal(t, "gpt-5.5", gjson.GetBytes(xhighBody, "model").String())
	require.Equal(t, "xhigh", gjson.GetBytes(xhighBody, "reasoning_effort").String())
}

func TestUnavailableModelsAreExcludedBeforeAutomaticMapping(t *testing.T) {
	metrics := []ModelMetric{
		{Model: "gpt-5.6-luna", Effort: "max", IQ: 98, CostUSD: 0.4, HasIQ: true, HasCost: true},
		{Model: "gpt-5.6-terra", Effort: "ultra", IQ: 104, CostUSD: 9, HasIQ: true, HasCost: true},
		{Model: "gpt-5.6-sol", Effort: "xhigh", IQ: 107, CostUSD: 6, HasIQ: true, HasCost: true},
	}
	available := filterUnavailableMetrics(metrics, []string{"GPT-5.6-LUNA"})
	plan, err := ComputeAutomaticMappings(available, AutoMappingOptions{
		BaselineModel: "gpt-5.6-luna",
		BaselineGap:   5,
		FormulaMode:   "natural_gap",
		GPTOnly:       true,
	})
	require.NoError(t, err)
	require.NotEqual(t, "gpt-5.6-luna", plan.Baseline.Model)
	for _, mapping := range plan.Mappings {
		require.NotEqual(t, "gpt-5.6-luna", mapping.ToModel)
	}
}

func TestNormalizeConfigMigratesAndNormalizesAutomaticAccountScope(t *testing.T) {
	normalized, err := NormalizeConfig(Config{
		Auto: AutoRoutingConfig{
			Enabled:            true,
			AccountIDs:         []int64{43, 42, 43, 0, -1},
			UnavailableModels:  []string{"gpt-5.6-sol", "GPT-5.6-SOL"},
			PausedTargetModels: []string{" GPT-5.6-LUNA ", "gpt-5.6-luna", ""},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{42, 43}, normalized.Auto.AccountIDs)
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.6-sol"}, normalized.Auto.UnavailableModels)
	require.Nil(t, normalized.Auto.PausedTargetModels)
	require.True(t, cloneConfig(normalized).Auto.Enabled)
	require.Equal(t, []int64{42, 43}, cloneConfig(normalized).Auto.AccountIDs)
	require.Equal(t, []string{"gpt-5.6-luna", "gpt-5.6-sol"}, cloneConfig(normalized).Auto.UnavailableModels)
	require.Nil(t, cloneConfig(normalized).Auto.PausedTargetModels)
}

func TestNormalizeAutomaticRefreshSchedule(t *testing.T) {
	normalized := normalizeAutoRoutingConfig(AutoRoutingConfig{
		RefreshTimes:     []string{"20.30", "07:30", "12:30", "07:30", "invalid"},
		ScheduleTimezone: "",
		FormulaMode:      "iq_cost",
		IQAggregation:    "max",
		IQWindowHours:    168,
	})
	require.Equal(t, []string{"30 7 * * *", "30 12 * * *", "30 20 * * *"}, normalized.CronSchedules)
	require.Nil(t, normalized.RefreshTimes)
	require.Equal(t, "Asia/Shanghai", normalized.ScheduleTimezone)
	require.Equal(t, "iq_cost", normalized.FormulaMode)
	require.Equal(t, "latest", normalized.IQAggregation)
	require.Zero(t, normalized.IQWindowHours)
}

func TestNextCronRefreshUsesDailyCronSchedules(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	today := time.Date(2026, time.August, 3, 8, 0, 0, 0, location)
	schedules := parseCronSchedules([]string{"30 7 * * *", "30 12 * * *", "30 20 * * *"})
	next := nextCronRefresh(today, schedules)
	require.Equal(t, time.Date(2026, time.August, 3, 12, 30, 0, 0, location), next)

	evening := time.Date(2026, time.August, 3, 21, 0, 0, 0, location)
	next = nextCronRefresh(evening, schedules)
	require.Equal(t, time.Date(2026, time.August, 4, 7, 30, 0, 0, location), next)
}

func TestTransformRequestPreservesAndRewritesBody(t *testing.T) {
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	require.NoError(t, SaveConfig(Config{Accounts: map[string]AccountConfig{
		"42": {Mappings: []Mapping{{From: "gpt-5", Effort: "low"}}},
	}}))

	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader([]byte(`{"model":"gpt-5","input":[]}`)))
	require.NoError(t, err)
	TransformRequest(req, 42)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), req.ContentLength)
	require.Equal(t, "low", gjson.GetBytes(body, "reasoning.effort").String())

	getBody, err := req.GetBody()
	require.NoError(t, err)
	getBodyBytes, err := io.ReadAll(getBody)
	require.NoError(t, err)
	require.Equal(t, body, getBodyBytes)
}

func TestResolveUsageMatchesRequestTransformation(t *testing.T) {
	t.Setenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG", t.TempDir()+"/config.json")
	require.NoError(t, SaveConfig(Config{Accounts: map[string]AccountConfig{
		"42": {Mappings: []Mapping{{From: "gpt-5", To: "gpt-5.2", Effort: "max"}}},
	}}))

	resolved := ResolveUsage(42, "gpt-5")
	require.Equal(t, UsageMapping{Model: "gpt-5.2", Effort: "max", Matched: true}, resolved)

	resolvedTarget := ResolveUsage(42, "gpt-5.2")
	require.Equal(t, UsageMapping{Model: "gpt-5.2", Effort: "max", Matched: true}, resolvedTarget)

	require.Equal(t, "gpt-5→gpt-5.2", UpdateUsageMappingChain("gpt-5→gpt-5", "gpt-5", "gpt-5", "gpt-5.2"))
	require.Equal(t, "request→gpt-5.2", UpdateUsageMappingChain("request→gpt-5", "request", "gpt-5", "gpt-5.2"))
}

func TestNormalizeConfigRejectsInvalidMappings(t *testing.T) {
	_, err := NormalizeConfig(Config{Accounts: map[string]AccountConfig{
		"42": {Mappings: []Mapping{{From: "gpt-5", Effort: "none"}}},
	}})
	require.Error(t, err)
}

func TestRegisteredImplementationsReceiveRuntimeCalls(t *testing.T) {
	Install()
	t.Cleanup(Install)

	requestCalled := false
	RegisterRequestTransformer(func(req *http.Request, accountID int64) {
		requestCalled = accountID == 42
		req.Header.Set("X-Plugin-Test", "request")
	})
	request, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", nil)
	require.NoError(t, err)
	TransformRequest(request, 42)
	require.True(t, requestCalled)
	require.Equal(t, "request", request.Header.Get("X-Plugin-Test"))

	RegisterUsageResolver(func(accountID int64, model string) UsageMapping {
		return UsageMapping{Model: model + "-plugin", Effort: "low", Matched: accountID == 42}
	})
	require.Equal(t, UsageMapping{Model: "gpt-plugin", Effort: "low", Matched: true}, ResolveUsage(42, "gpt"))
}
