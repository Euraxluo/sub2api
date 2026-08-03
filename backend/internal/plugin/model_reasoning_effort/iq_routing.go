package model_reasoning_effort

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

const (
	defaultRadarBaseURL       = "https://api.codexradar.com"
	defaultRefreshMinutes     = 360
	defaultRadarTimeoutSecond = 30
	minRefreshMinutes         = 5
	maxRefreshMinutes         = 24 * 60
	maxRadarTimeoutSecond     = 120
	maxIQHistoryBytes         = 16 << 20
	maxRadarTableBytes        = 32 << 20
	defaultBaselineModel      = "gpt-5.6-luna"
	defaultBaselineEffort     = "max"
	defaultBaselineGap        = 5.0
	gptModelPattern           = "gpt-*"
	defaultScheduleTimezone   = "Asia/Shanghai"
)

// ModelMetric is the normalized IQ/cost record used by the routing formula.
// Cost is optional because a fresh IQ entry may not have a completed cost
// sample yet; such an entry can be a source but never becomes a cheap target.
type ModelMetric struct {
	Model       string  `json:"model"`
	Effort      string  `json:"effort"`
	IQ          float64 `json:"iq"`
	HasIQ       bool    `json:"has_iq"`
	CostUSD     float64 `json:"cost_usd,omitempty"`
	HasCost     bool    `json:"has_cost"`
	IQSamples   int     `json:"iq_samples,omitempty"`
	CostSamples int     `json:"cost_samples,omitempty"`
}

// AutoMappingOptions is the pure formula configuration. GPTOnly is kept
// explicit even though the current plugin always enables it, so future model
// families cannot silently enter this strategy.
type AutoMappingOptions struct {
	BaselineModel string
	BaselineGap   float64
	IQAggregation string
	IQWindow      time.Duration
	GPTOnly       bool
}

type IQBand struct {
	Name         string  `json:"name"`
	MinIQ        float64 `json:"min_iq"`
	MaxIQ        float64 `json:"max_iq"`
	TargetModel  string  `json:"target_model,omitempty"`
	TargetEffort string  `json:"target_effort,omitempty"`
}

// AutoMappingPlan is the auditable result of one formula evaluation.
type AutoMappingPlan struct {
	GeneratedAt   time.Time   `json:"generated_at"`
	Baseline      ModelMetric `json:"baseline"`
	BaselineLimit float64     `json:"baseline_limit"`
	MaxIQ         float64     `json:"max_iq"`
	Bands         []IQBand    `json:"bands"`
	Mappings      []Mapping   `json:"mappings"`
}

type AutoRoutingSnapshot struct {
	Plan          AutoMappingPlan `json:"plan"`
	Metrics       []ModelMetric   `json:"metrics"`
	LastRefreshAt time.Time       `json:"last_refresh_at,omitempty"`
	LastError     string          `json:"last_error,omitempty"`
}

var (
	autoSnapshot     atomic.Pointer[AutoRoutingSnapshot]
	autoRunnerMu     sync.Mutex
	autoRunnerCancel context.CancelFunc
)

var standardCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func normalizeAutoRoutingConfig(value AutoRoutingConfig) AutoRoutingConfig {
	value.AccountIDs = normalizeAutoAccountIDs(value.AccountIDs)
	value.RefreshTimes = normalizeRefreshTimes(value.RefreshTimes)
	value.CronSchedules = normalizeCronSchedules(value.CronSchedules)
	if len(value.CronSchedules) == 0 && len(value.RefreshTimes) > 0 {
		value.CronSchedules = refreshTimesToCronSchedules(value.RefreshTimes)
	}
	value.RefreshTimes = nil
	value.ScheduleTimezone = normalizeScheduleTimezone(value.ScheduleTimezone)
	if strings.TrimSpace(value.RadarBaseURL) == "" {
		value.RadarBaseURL = defaultRadarBaseURL
	}
	if value.RefreshIntervalMinutes <= 0 {
		value.RefreshIntervalMinutes = defaultRefreshMinutes
	}
	if value.RefreshIntervalMinutes < minRefreshMinutes {
		value.RefreshIntervalMinutes = minRefreshMinutes
	}
	if value.RefreshIntervalMinutes > maxRefreshMinutes {
		value.RefreshIntervalMinutes = maxRefreshMinutes
	}
	if value.TimeoutSeconds <= 0 {
		value.TimeoutSeconds = defaultRadarTimeoutSecond
	}
	if value.TimeoutSeconds > maxRadarTimeoutSecond {
		value.TimeoutSeconds = maxRadarTimeoutSecond
	}
	value.IQAggregation = strings.ToLower(strings.TrimSpace(value.IQAggregation))
	if value.IQAggregation != "max" && value.IQAggregation != "latest" && value.IQAggregation != "mean" {
		value.IQAggregation = "max"
	}
	if value.IQWindowHours < 0 {
		value.IQWindowHours = 0
	}
	if strings.TrimSpace(value.BaselineModel) == "" {
		value.BaselineModel = defaultBaselineModel
	}
	if value.BaselineGap <= 0 {
		value.BaselineGap = defaultBaselineGap
	}
	return value
}

func normalizeScheduleTimezone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultScheduleTimezone
	}
	if _, err := time.LoadLocation(value); err != nil {
		return defaultScheduleTimezone
	}
	return value
}

func normalizeRefreshTimes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	minutes := make(map[int]struct{}, len(values))
	for _, value := range values {
		hour, minute, ok := parseRefreshTime(value)
		if !ok {
			continue
		}
		minutes[hour*60+minute] = struct{}{}
	}
	if len(minutes) == 0 {
		return nil
	}
	ordered := make([]int, 0, len(minutes))
	for value := range minutes {
		ordered = append(ordered, value)
	}
	sort.Ints(ordered)
	result := make([]string, 0, len(ordered))
	for _, value := range ordered {
		result = append(result, fmt.Sprintf("%02d:%02d", value/60, value%60))
	}
	return result
}

func parseRefreshTime(value string) (hour, minute int, ok bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ".", ":"))
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	minute, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func firstInvalidCronSchedule(values []string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := standardCronParser.Parse(value); err != nil {
			return value
		}
	}
	return ""
}

func normalizeCronSchedules(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, err := standardCronParser.Parse(value); err != nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func refreshTimesToCronSchedules(values []string) []string {
	times := normalizeRefreshTimes(values)
	result := make([]string, 0, len(times))
	for _, value := range times {
		hour, minute, ok := parseRefreshTime(value)
		if ok {
			result = append(result, fmt.Sprintf("%d %d * * *", minute, hour))
		}
	}
	return normalizeCronSchedules(result)
}

func normalizeAutoAccountIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	if len(result) == 0 {
		return nil
	}
	return result
}

func startAutoRefresh() {
	config, err := LoadConfig()
	if err != nil {
		log.Printf("[model-reasoning-effort] auto routing config unavailable: %v", err)
		return
	}

	autoRunnerMu.Lock()
	if autoRunnerCancel != nil {
		autoRunnerCancel()
		autoRunnerCancel = nil
	}
	if !config.Auto.Enabled {
		autoSnapshot.Store(nil)
		autoRunnerMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	autoRunnerCancel = cancel
	options := normalizeAutoRoutingConfig(config.Auto)
	autoRunnerMu.Unlock()

	go runAutoRefresh(ctx, options)
}

func runAutoRefresh(ctx context.Context, config AutoRoutingConfig) {
	refresh := func() {
		if err := RefreshAutoMappings(ctx); err != nil && !errors.Is(err, context.Canceled) {
			recordAutoRefreshError(err)
			log.Printf("[model-reasoning-effort] auto routing refresh failed: %v", err)
		}
	}
	refresh()
	if len(config.CronSchedules) > 0 {
		runCronAutoRefresh(ctx, config, refresh)
		return
	}

	interval := time.Duration(config.RefreshIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}

func runCronAutoRefresh(ctx context.Context, config AutoRoutingConfig, refresh func()) {
	location := scheduleLocation(config.ScheduleTimezone)
	schedules := parseCronSchedules(config.CronSchedules)
	if len(schedules) == 0 {
		return
	}
	for {
		now := time.Now().In(location)
		next := nextCronRefresh(now, schedules)
		delay := time.Until(next)
		if delay <= 0 {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			refresh()
		}
	}
}

func scheduleLocation(timezone string) *time.Location {
	location, err := time.LoadLocation(normalizeScheduleTimezone(timezone))
	if err != nil {
		return time.Local
	}
	return location
}

func parseCronSchedules(values []string) []cron.Schedule {
	result := make([]cron.Schedule, 0, len(values))
	for _, value := range normalizeCronSchedules(values) {
		schedule, err := standardCronParser.Parse(value)
		if err == nil {
			result = append(result, schedule)
		}
	}
	return result
}

func nextCronRefresh(now time.Time, schedules []cron.Schedule) time.Time {
	best := time.Time{}
	for _, schedule := range schedules {
		candidate := schedule.Next(now)
		if best.IsZero() || candidate.Before(best) {
			best = candidate
		}
	}
	if best.IsZero() {
		return now.Add(24 * time.Hour)
	}
	return best
}

// RefreshAutoMappings fetches the public Radar feeds and publishes a complete
// immutable snapshot. A failed refresh never destroys the previous snapshot.
func RefreshAutoMappings(ctx context.Context) error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if !config.Auto.Enabled {
		return errors.New("automatic IQ routing is disabled")
	}
	metrics, err := FetchRadarMetrics(ctx, normalizeAutoRoutingConfig(config.Auto))
	if err != nil {
		return err
	}
	plan, err := ComputeAutomaticMappings(metrics, AutoMappingOptions{
		BaselineModel: config.Auto.BaselineModel,
		BaselineGap:   config.Auto.BaselineGap,
		IQAggregation: config.Auto.IQAggregation,
		IQWindow:      time.Duration(config.Auto.IQWindowHours) * time.Hour,
		GPTOnly:       true,
	})
	if err != nil {
		return err
	}
	autoSnapshot.Store(&AutoRoutingSnapshot{
		Plan:          clonePlan(plan),
		Metrics:       append([]ModelMetric(nil), metrics...),
		LastRefreshAt: time.Now().UTC(),
	})
	return nil
}

func recordAutoRefreshError(err error) {
	current := AutoRoutingStatus()
	current.LastError = err.Error()
	autoSnapshot.Store(&current)
}

func currentAutoMappings() []Mapping {
	snapshot := autoSnapshot.Load()
	if snapshot == nil {
		return nil
	}
	return append([]Mapping(nil), snapshot.Plan.Mappings...)
}

// AutoMappings returns the current generated mappings for an admin view or a
// future scheduler hook. The returned slice is detached from the live state.
func AutoMappings() []Mapping {
	mappings := currentAutoMappings()
	if len(mappings) > 0 {
		return mappings
	}
	config, err := LoadConfig()
	if err != nil || !config.Auto.Enabled {
		return nil
	}
	auto := normalizeAutoRoutingConfig(config.Auto)
	return automaticCoverageMappings(auto.BaselineModel, defaultBaselineEffort)
}

// AutoRoutingStatus returns a detached copy of the latest generated snapshot.
func AutoRoutingStatus() AutoRoutingSnapshot {
	snapshot := autoSnapshot.Load()
	if snapshot == nil {
		return AutoRoutingSnapshot{}
	}
	return AutoRoutingSnapshot{
		Plan:          clonePlan(snapshot.Plan),
		Metrics:       append([]ModelMetric(nil), snapshot.Metrics...),
		LastRefreshAt: snapshot.LastRefreshAt,
		LastError:     snapshot.LastError,
	}
}

func clonePlan(plan AutoMappingPlan) AutoMappingPlan {
	plan.Bands = append([]IQBand(nil), plan.Bands...)
	plan.Mappings = append([]Mapping(nil), plan.Mappings...)
	return plan
}

// ComputeAutomaticMappings implements the requested formula:
//  1. Filter to GPT configurations only.
//  2. Pick the highest-IQ Luna configuration as L.
//  3. Map every q <= L+5 directly to L.
//  4. Split the remaining distribution at its largest natural IQ gap.
//  5. Map each remaining band to its lowest-cost configuration.
//  6. Add a GPT fallback so an unobserved model or effort also maps to L.
func ComputeAutomaticMappings(metrics []ModelMetric, options AutoMappingOptions) (AutoMappingPlan, error) {
	options = normalizeMappingOptions(options)
	filtered := deduplicateMetrics(metrics, options)
	if len(filtered) == 0 {
		return AutoMappingPlan{}, errors.New("no GPT IQ metrics available")
	}

	baselineCandidates := make([]ModelMetric, 0)
	for _, metric := range filtered {
		if metric.HasIQ && isLunaModel(metric.Model) {
			baselineCandidates = append(baselineCandidates, metric)
		}
	}
	if len(baselineCandidates) == 0 {
		// Keep the configured model as a compatibility fallback for older Radar
		// snapshots that did not label Luna models consistently.
		for _, metric := range filtered {
			if metric.HasIQ && strings.EqualFold(metric.Model, options.BaselineModel) {
				baselineCandidates = append(baselineCandidates, metric)
			}
		}
	}
	if len(baselineCandidates) == 0 {
		return AutoMappingPlan{}, fmt.Errorf("no Luna IQ metric available for baseline model %q", options.BaselineModel)
	}
	sort.Slice(baselineCandidates, func(i, j int) bool {
		return metricBetterForBaseline(baselineCandidates[i], baselineCandidates[j])
	})
	baseline := baselineCandidates[0]
	limit := baseline.IQ + options.BaselineGap

	plan := AutoMappingPlan{
		GeneratedAt:   time.Now().UTC(),
		Baseline:      baseline,
		BaselineLimit: limit,
		MaxIQ:         maxMetricIQ(filtered),
		Bands: []IQBand{{
			Name:         "baseline_or_lower",
			MinIQ:        math.Inf(-1),
			MaxIQ:        limit,
			TargetModel:  baseline.Model,
			TargetEffort: baseline.Effort,
		}},
	}

	remaining := make([]ModelMetric, 0, len(filtered))
	for _, metric := range filtered {
		if metric.IQ > limit {
			remaining = append(remaining, metric)
		}
	}
	if len(remaining) > 0 {
		middle, higher, split := splitIQDistribution(remaining)
		if len(middle) > 0 {
			plan.Bands = append(plan.Bands, bandForMetrics("middle", middle))
		}
		if len(higher) > 0 {
			plan.Bands = append(plan.Bands, bandForMetrics("higher", higher))
		}
		_ = split // split is represented by the two band's min/max values.
	}

	for _, metric := range filtered {
		band := bandForMetric(plan.Bands, metric.IQ)
		if !metric.HasIQ {
			plan.Mappings = append(plan.Mappings, Mapping{
				FromModel:  metric.Model,
				FromEffort: metric.Effort,
				ToModel:    baseline.Model,
				ToEffort:   baseline.Effort,
			})
			continue
		}
		if band == nil || band.TargetModel == "" || band.TargetEffort == "" {
			continue
		}
		plan.Mappings = append(plan.Mappings, Mapping{
			FromModel:  metric.Model,
			FromEffort: metric.Effort,
			ToModel:    band.TargetModel,
			ToEffort:   band.TargetEffort,
		})
	}
	plan.Mappings = expandAutomaticCoverage(plan.Mappings, baseline.Model, baseline.Effort)
	sort.Slice(plan.Mappings, func(i, j int) bool {
		return mappingKey(plan.Mappings[i]) < mappingKey(plan.Mappings[j])
	})
	return plan, nil
}

func normalizeMappingOptions(options AutoMappingOptions) AutoMappingOptions {
	if strings.TrimSpace(options.BaselineModel) == "" {
		options.BaselineModel = defaultBaselineModel
	}
	if options.BaselineGap <= 0 {
		options.BaselineGap = defaultBaselineGap
	}
	options.IQAggregation = strings.ToLower(strings.TrimSpace(options.IQAggregation))
	if options.IQAggregation != "max" && options.IQAggregation != "latest" && options.IQAggregation != "mean" {
		options.IQAggregation = "max"
	}
	return options
}

func deduplicateMetrics(metrics []ModelMetric, options AutoMappingOptions) []ModelMetric {
	byKey := make(map[string]ModelMetric, len(metrics))
	for _, raw := range metrics {
		metric := raw
		metric.Model = normalizeModelName(metric.Model)
		metric.Effort = normalize(metric.Effort)
		if metric.Effort == "" {
			metric.Effort = defaultReasoningEffort
		}
		if !metric.HasIQ {
			metric.HasIQ = isFinitePositive(metric.IQ)
		}
		if metric.Model == "" || !isGPTModel(metric.Model, options.GPTOnly) || (metric.HasIQ && !isFinitePositive(metric.IQ)) {
			continue
		}
		key := metricKey(metric.Model, metric.Effort)
		if previous, ok := byKey[key]; !ok || metric.IQ > previous.IQ || (metric.IQ == previous.IQ && metric.HasCost && !previous.HasCost) {
			byKey[key] = metric
		}
	}
	result := make([]ModelMetric, 0, len(byKey))
	for _, metric := range byKey {
		result = append(result, metric)
	}
	sort.Slice(result, func(i, j int) bool {
		return metricKey(result[i].Model, result[i].Effort) < metricKey(result[j].Model, result[j].Effort)
	})
	return result
}

func isGPTModel(model string, gptOnly bool) bool {
	if !gptOnly {
		return true
	}
	value := strings.ToLower(strings.TrimSpace(model))
	if !strings.HasPrefix(value, "gpt-") {
		return false
	}
	version := strings.TrimPrefix(value, "gpt-")
	majorEnd := 0
	for majorEnd < len(version) && isASCIIDigit(version[majorEnd]) {
		majorEnd++
	}
	if majorEnd == 0 {
		return false
	}
	if majorEnd == len(version) {
		return true
	}
	if version[majorEnd] != '.' {
		return false
	}
	minorEnd := majorEnd + 1
	for minorEnd < len(version) && isASCIIDigit(version[minorEnd]) {
		minorEnd++
	}
	if minorEnd == majorEnd+1 {
		return false
	}
	return minorEnd == len(version) || version[minorEnd] == '-'
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func isLunaModel(model string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(model)), "-")
	for _, part := range parts {
		if part == "luna" {
			return true
		}
	}
	return false
}

func metricBetterForBaseline(left, right ModelMetric) bool {
	if left.IQ != right.IQ {
		return left.IQ > right.IQ
	}
	if left.HasCost != right.HasCost {
		return left.HasCost
	}
	if left.HasCost && left.CostUSD != right.CostUSD {
		return left.CostUSD < right.CostUSD
	}
	return mappingKey(Mapping{FromModel: left.Model, FromEffort: left.Effort}) < mappingKey(Mapping{FromModel: right.Model, FromEffort: right.Effort})
}

func maxMetricIQ(metrics []ModelMetric) float64 {
	max := 0.0
	for _, metric := range metrics {
		if metric.HasIQ && metric.IQ > max {
			max = metric.IQ
		}
	}
	return max
}

func splitIQDistribution(metrics []ModelMetric) (middle, higher []ModelMetric, split float64) {
	sort.Slice(metrics, func(i, j int) bool {
		if metrics[i].IQ != metrics[j].IQ {
			return metrics[i].IQ < metrics[j].IQ
		}
		return metricKey(metrics[i].Model, metrics[i].Effort) < metricKey(metrics[j].Model, metrics[j].Effort)
	})
	unique := make([]float64, 0, len(metrics))
	for _, metric := range metrics {
		if len(unique) == 0 || metric.IQ != unique[len(unique)-1] {
			unique = append(unique, metric.IQ)
		}
	}
	if len(unique) < 2 {
		return nil, metrics, unique[0]
	}
	bestGap := -1.0
	bestIndex := 0
	for i := 1; i < len(unique); i++ {
		gap := unique[i] - unique[i-1]
		if gap > bestGap {
			bestGap = gap
			bestIndex = i - 1
		}
	}
	split = unique[bestIndex]
	for _, metric := range metrics {
		if metric.IQ <= split {
			middle = append(middle, metric)
		} else {
			higher = append(higher, metric)
		}
	}
	return middle, higher, split
}

func bandForMetrics(name string, metrics []ModelMetric) IQBand {
	minIQ, maxIQ := metrics[0].IQ, metrics[0].IQ
	for _, metric := range metrics[1:] {
		minIQ = math.Min(minIQ, metric.IQ)
		maxIQ = math.Max(maxIQ, metric.IQ)
	}
	target, ok := cheapestMetric(metrics)
	band := IQBand{Name: name, MinIQ: minIQ, MaxIQ: maxIQ}
	if ok {
		band.TargetModel = target.Model
		band.TargetEffort = target.Effort
	}
	return band
}

func bandForMetric(bands []IQBand, iq float64) *IQBand {
	for i := range bands {
		band := &bands[i]
		if iq >= band.MinIQ && iq <= band.MaxIQ {
			return band
		}
	}
	return nil
}

func cheapestMetric(metrics []ModelMetric) (ModelMetric, bool) {
	var best ModelMetric
	found := false
	for _, metric := range metrics {
		if !metric.HasCost || !isFiniteNonNegative(metric.CostUSD) {
			continue
		}
		if !found || metric.CostUSD < best.CostUSD || (metric.CostUSD == best.CostUSD && metricKey(metric.Model, metric.Effort) < metricKey(best.Model, best.Effort)) {
			best = metric
			found = true
		}
	}
	return best, found
}

func mappingKey(mapping Mapping) string {
	mapping = canonicalMapping(mapping)
	return mapping.FromModel + "@" + mapping.FromEffort + "->" + mapping.ToModel + "@" + mapping.ToEffort
}

func metricKey(model, effort string) string {
	return strings.TrimSpace(model) + "@" + normalize(effort)
}

func normalizeModelName(model string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(model), "latest:"))
}

func automaticCoverageMappings(baselineModel, baselineEffort string) []Mapping {
	baselineModel = normalizeModelName(baselineModel)
	if baselineModel == "" {
		baselineModel = defaultBaselineModel
	}
	baselineEffort = normalize(baselineEffort)
	if baselineEffort == "" {
		baselineEffort = defaultBaselineEffort
	}
	return []Mapping{{
		FromModel: gptModelPattern,
		ToModel:   baselineModel,
		ToEffort:  baselineEffort,
	}}
}

func expandAutomaticCoverage(mappings []Mapping, baselineModel, baselineEffort string) []Mapping {
	result := append([]Mapping(nil), mappings...)
	for _, mapping := range result {
		canonical := canonicalMapping(mapping)
		if canonical.FromModel == gptModelPattern && canonical.FromEffort == "" {
			return result
		}
	}
	return append(result, automaticCoverageMappings(baselineModel, baselineEffort)...)
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

type radarIQPoint struct {
	TS    string  `json:"ts"`
	Score float64 `json:"score"`
	IQ    float64 `json:"iq"`
}

type radarTableCell struct {
	N       int      `json:"n"`
	TotalN  int      `json:"total_n"`
	Cost    *float64 `json:"cost"`
	AvgCost *float64 `json:"average_cost_usd"`
}

type radarTableEnvelope struct {
	Cells map[string]radarTableCell `json:"cells"`
}

// FetchRadarMetrics reads the public IQ history and task table. The table is
// deliberately fetched only on the slow refresh loop; it is too large for the
// request hot path.
func FetchRadarMetrics(ctx context.Context, config AutoRoutingConfig) ([]ModelMetric, error) {
	config = normalizeAutoRoutingConfig(config)
	base, err := validateRadarBaseURL(config.RadarBaseURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Duration(config.TimeoutSeconds) * time.Second}
	historyBody, err := fetchRadarJSON(ctx, client, base+"/api/v1/iq-history", maxIQHistoryBytes)
	if err != nil {
		return nil, err
	}
	tableBody, err := fetchRadarJSON(ctx, client, base+"/api/v1/table", maxRadarTableBytes)
	if err != nil {
		return nil, err
	}
	return mergeRadarMetrics(historyBody, tableBody, config), nil
}

func validateRadarBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid Radar base URL")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func fetchRadarJSON(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("Radar request %s returned HTTP %d", endpoint, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("Radar response %s exceeds %d bytes", endpoint, limit)
	}
	return data, nil
}

func mergeRadarMetrics(historyBody, tableBody []byte, config AutoRoutingConfig) []ModelMetric {
	iq := parseRadarIQHistory(historyBody, config)
	costs := parseRadarTableCosts(tableBody)
	metrics := make([]ModelMetric, 0, len(iq))
	for key, score := range iq {
		model, effort := splitRadarModelKey(key)
		if model == "" || !isGPTModel(model, true) || !isFinitePositive(score.IQ) {
			continue
		}
		metric := ModelMetric{Model: model, Effort: effort, IQ: score.IQ, IQSamples: score.Samples}
		if cost, ok := costs[key]; ok {
			metric.CostUSD = cost.Cost
			metric.HasCost = true
			metric.CostSamples = cost.Samples
		}
		metrics = append(metrics, metric)
	}
	sort.Slice(metrics, func(i, j int) bool {
		return metricKey(metrics[i].Model, metrics[i].Effort) < metricKey(metrics[j].Model, metrics[j].Effort)
	})
	return metrics
}

type radarIQScore struct {
	IQ      float64
	Samples int
}

func parseRadarIQHistory(data []byte, config AutoRoutingConfig) map[string]radarIQScore {
	var raw map[string][]radarIQPoint
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	result := make(map[string]radarIQScore)
	for rawKey, points := range raw {
		key := strings.TrimPrefix(strings.TrimSpace(rawKey), "latest:")
		model, effort := splitRadarModelKey(key)
		if model == "" || !isGPTModel(model, true) {
			continue
		}
		score, ok := aggregateIQPoints(points, config)
		if !ok {
			continue
		}
		canonicalKey := metricKey(model, effort)
		// The non-latest history key is preferred when both forms are present.
		if _, exists := result[canonicalKey]; !exists || !strings.HasPrefix(rawKey, "latest:") {
			result[canonicalKey] = score
		}
	}
	return result
}

func aggregateIQPoints(points []radarIQPoint, config AutoRoutingConfig) (radarIQScore, bool) {
	type validPoint struct {
		score float64
		at    time.Time
	}
	valid := make([]validPoint, 0, len(points))
	for _, point := range points {
		score := point.Score
		if !isFinitePositive(score) {
			score = point.IQ
		}
		if !isFinitePositive(score) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, strings.TrimSpace(point.TS))
		valid = append(valid, validPoint{score: score, at: at})
	}
	if len(valid) == 0 {
		return radarIQScore{}, false
	}
	if config.IQWindowHours > 0 {
		latest := valid[0].at
		for _, point := range valid[1:] {
			if point.at.After(latest) {
				latest = point.at
			}
		}
		cutoff := latest.Add(-time.Duration(config.IQWindowHours) * time.Hour)
		filtered := valid[:0]
		for _, point := range valid {
			if point.at.IsZero() || !point.at.Before(cutoff) {
				filtered = append(filtered, point)
			}
		}
		if len(filtered) > 0 {
			valid = filtered
		}
	}
	score := valid[0].score
	switch config.IQAggregation {
	case "latest":
		for _, point := range valid[1:] {
			if point.at.After(valid[0].at) {
				valid[0] = point
			}
		}
		score = valid[0].score
	case "mean":
		sum := 0.0
		for _, point := range valid {
			sum += point.score
		}
		score = sum / float64(len(valid))
	default:
		for _, point := range valid[1:] {
			if point.score > score {
				score = point.score
			}
		}
	}
	return radarIQScore{IQ: score, Samples: len(valid)}, true
}

type radarCost struct {
	Cost    float64
	Samples int
}

func parseRadarTableCosts(data []byte) map[string]radarCost {
	var envelope radarTableEnvelope
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Cells) == 0 {
		return nil
	}
	totals := make(map[string]radarCost)
	for rawKey, cell := range envelope.Cells {
		parts := strings.Split(rawKey, "|")
		if len(parts) < 3 {
			continue
		}
		model := strings.TrimSpace(parts[len(parts)-2])
		effort := normalize(parts[len(parts)-1])
		if model == "" || effort == "" || !isGPTModel(model, true) {
			continue
		}
		var cost *float64
		if cell.Cost != nil {
			cost = cell.Cost
		} else {
			cost = cell.AvgCost
		}
		if cost == nil || !isFiniteNonNegative(*cost) {
			continue
		}
		weight := cell.TotalN
		if weight <= 0 {
			weight = cell.N
		}
		if weight <= 0 {
			weight = 1
		}
		key := metricKey(model, effort)
		current := totals[key]
		current.Cost += *cost * float64(weight)
		current.Samples += weight
		totals[key] = current
	}
	for key, total := range totals {
		if total.Samples > 0 {
			total.Cost /= float64(total.Samples)
			totals[key] = total
		}
	}
	return totals
}

func splitRadarModelKey(raw string) (string, string) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "latest:")
	if raw == "" {
		return "", ""
	}
	if model, effort, ok := strings.Cut(raw, "@"); ok {
		effort = normalize(effort)
		if effort == "" {
			return "", ""
		}
		return strings.TrimSpace(model), effort
	}
	return raw, defaultReasoningEffort
}
