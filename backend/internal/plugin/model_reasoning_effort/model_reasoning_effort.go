// Package model_reasoning_effort owns the Admin Tools model-to-effort plugin.
// The gateway integration is intentionally a single HTTP transport hook.
package model_reasoning_effort

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	ConfigFileName         = "model-reasoning-effort.json"
	maxMappings            = 128
	defaultReasoningEffort = "medium"
)

var supportedEfforts = map[string]struct{}{
	"minimal": {},
	"low":     {},
	"medium":  {},
	"high":    {},
	"xhigh":   {},
	"ultra":   {},
	"max":     {},
}

// Mapping is one request-model rule. A canonical rule is keyed by both the
// source model and source effort. The legacy fields remain available to Go
// callers compiled against the first version of this plugin; they are only
// accepted as input and are written back using the canonical JSON names.
type Mapping struct {
	FromModel  string `json:"from_model,omitempty"`
	FromEffort string `json:"from_effort,omitempty"`
	ToModel    string `json:"to_model,omitempty"`
	ToEffort   string `json:"to_effort,omitempty"`

	// Deprecated compatibility aliases. NormalizeConfig converts these to the
	// four canonical fields before they are used or persisted.
	From   string `json:"-"`
	To     string `json:"-"`
	Effort string `json:"-"`

	legacy bool
}

func (m Mapping) MarshalJSON() ([]byte, error) {
	m = canonicalMapping(m)
	type wireMapping struct {
		FromModel  string `json:"from_model,omitempty"`
		FromEffort string `json:"from_effort,omitempty"`
		ToModel    string `json:"to_model,omitempty"`
		ToEffort   string `json:"to_effort,omitempty"`
	}
	return json.Marshal(wireMapping{
		FromModel:  m.FromModel,
		FromEffort: m.FromEffort,
		ToModel:    m.ToModel,
		ToEffort:   m.ToEffort,
	})
}

func (m *Mapping) UnmarshalJSON(data []byte) error {
	type wireMapping struct {
		FromModel  string `json:"from_model"`
		FromEffort string `json:"from_effort"`
		ToModel    string `json:"to_model"`
		ToEffort   string `json:"to_effort"`
		From       string `json:"from"`
		To         string `json:"to"`
		Effort     string `json:"effort"`
	}
	var wire wireMapping
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*m = Mapping{
		FromModel:  firstNonEmpty(wire.FromModel, wire.From),
		FromEffort: firstNonEmpty(wire.FromEffort, ""),
		ToModel:    firstNonEmpty(wire.ToModel, wire.To),
		ToEffort:   firstNonEmpty(wire.ToEffort, wire.Effort),
		legacy:     wire.From != "" || wire.To != "" || wire.Effort != "",
	}
	return nil
}

// UsageMapping is the effective model/effort state after this plugin's
// request transformation. It is used by billing and usage presentation so
// those records describe the same request that reached the upstream.
type UsageMapping struct {
	Model   string
	Effort  string
	Matched bool
}

// RequestTransformer is the process-wide request transformation hook.
// Keeping the type in the plugin package lets future plugins replace the
// implementation without making the transport depend on plugin internals.
type RequestTransformer func(req *http.Request, accountID int64)

// UsageResolver is the process-wide usage metadata hook. It must use the same
// mapping semantics as RequestTransformer so billing follows the sent request.
type UsageResolver func(accountID int64, model string) UsageMapping

// EffortUsageResolver is the exact usage hook. It receives the source effort
// so billing cannot collapse sol@xhigh and sol@max into one model rule.
type EffortUsageResolver func(accountID int64, model, effort string) UsageMapping

var (
	requestTransformer  atomic.Value // stores RequestTransformer
	usageResolver       atomic.Value // stores UsageResolver
	effortUsageResolver atomic.Value // stores EffortUsageResolver
)

func init() {
	requestTransformer.Store(RequestTransformer(transformRequest))
	usageResolver.Store(UsageResolver(resolveUsage))
	effortUsageResolver.Store(EffortUsageResolver(resolveUsageWithEffort))
	Install()
}

// Install registers this plugin's default implementations in the explicit
// process-wide registry. Calling it again restores the built-in handlers.
func Install() {
	RegisterRequestTransformer(transformRequest)
	RegisterUsageResolver(resolveUsage)
	RegisterEffortUsageResolver(resolveUsageWithEffort)
	startAutoRefresh()
}

// RegisterRequestTransformer replaces the request implementation. A nil
// handler restores the built-in implementation.
func RegisterRequestTransformer(handler RequestTransformer) {
	if handler == nil {
		handler = transformRequest
	}
	requestTransformer.Store(handler)
}

// RegisterUsageResolver replaces the usage implementation. A nil handler
// restores the built-in implementation.
func RegisterUsageResolver(handler UsageResolver) {
	if handler == nil {
		handler = resolveUsage
	}
	usageResolver.Store(handler)
}

// RegisterEffortUsageResolver replaces the exact usage implementation. A nil
// handler restores the built-in implementation.
func RegisterEffortUsageResolver(handler EffortUsageResolver) {
	if handler == nil {
		handler = resolveUsageWithEffort
	}
	effortUsageResolver.Store(handler)
}

type AccountConfig struct {
	Mappings []Mapping `json:"mappings"`
}

// AutoRoutingConfig controls the plugin-owned IQ refresh loop. It is kept in
// the same JSON file as manual mappings so the host application needs no new
// configuration plumbing.
type AutoRoutingConfig struct {
	Enabled            bool     `json:"enabled"`
	AccountIDs         []int64  `json:"account_ids,omitempty"`
	UnavailableModels  []string `json:"unavailable_models,omitempty"`
	PausedTargetModels []string `json:"paused_target_models,omitempty"`
	CronSchedules      []string `json:"cron_schedules,omitempty"`
	// RefreshTimes is retained as a migration input for the previous UI.
	RefreshTimes           []string `json:"refresh_times,omitempty"`
	ScheduleTimezone       string   `json:"schedule_timezone,omitempty"`
	RadarBaseURL           string   `json:"radar_base_url,omitempty"`
	RefreshIntervalMinutes int      `json:"refresh_interval_minutes,omitempty"`
	TimeoutSeconds         int      `json:"timeout_seconds,omitempty"`
	// IQAggregation and IQWindowHours are retained for config compatibility;
	// normalization always uses the newest point from Radar's latest: series.
	IQAggregation string  `json:"iq_aggregation,omitempty"`
	IQWindowHours int     `json:"iq_window_hours,omitempty"`
	FormulaMode   string  `json:"formula_mode,omitempty"`
	BaselineModel string  `json:"baseline_model,omitempty"`
	BaselineGap   float64 `json:"baseline_gap,omitempty"`
}

type Config struct {
	Accounts map[string]AccountConfig `json:"accounts"`
	Auto     AutoRoutingConfig        `json:"auto,omitempty"`
}

var configState struct {
	sync.RWMutex
	loaded bool
	path   string
	value  Config
}

// LoadConfig reads the plugin-owned persistent configuration.
func LoadConfig() (Config, error) {
	path := configPath()
	configState.RLock()
	if configState.loaded && configState.path == path {
		value := cloneConfig(configState.value)
		configState.RUnlock()
		return value, nil
	}
	configState.RUnlock()

	configState.Lock()
	defer configState.Unlock()
	if configState.loaded && configState.path == path {
		return cloneConfig(configState.value), nil
	}

	value := Config{Accounts: map[string]AccountConfig{}}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &value); err != nil {
			return Config{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	value, err = NormalizeConfig(value)
	if err != nil {
		return Config{}, err
	}
	configState.value = value
	configState.path = path
	configState.loaded = true
	return cloneConfig(value), nil
}

// SaveConfig validates and persists plugin configuration.
func SaveConfig(value Config) error {
	value, err := NormalizeConfig(value)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".model-reasoning-effort-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	configState.Lock()
	configState.value = value
	configState.path = path
	configState.loaded = true
	configState.Unlock()
	startAutoRefresh()
	return nil
}

// NormalizeConfig validates plugin-owned configuration before it reaches the
// request path. Empty rows are ignored so the UI can keep draft rows locally.
func NormalizeConfig(value Config) (Config, error) {
	if value.Accounts == nil {
		value.Accounts = map[string]AccountConfig{}
	}
	for accountID, account := range value.Accounts {
		if _, err := strconv.ParseInt(accountID, 10, 64); err != nil {
			return Config{}, errors.New("model reasoning config contains an invalid account id")
		}
		if len(account.Mappings) > maxMappings {
			return Config{}, errors.New("model reasoning mappings exceed the limit")
		}
		normalized := make([]Mapping, 0, len(account.Mappings))
		for _, mapping := range account.Mappings {
			rawFromEffort := strings.TrimSpace(firstNonEmpty(mapping.FromEffort, ""))
			if rawFromEffort != "" && normalize(rawFromEffort) == "" {
				return Config{}, errors.New("model reasoning mapping contains an invalid from_effort")
			}
			mapping = canonicalMapping(mapping)
			if mapping.FromModel == "" && mapping.ToModel == "" && mapping.FromEffort == "" && mapping.ToEffort == "" {
				continue
			}
			if mapping.FromModel == "" || mapping.ToEffort == "" {
				return Config{}, errors.New("each model reasoning mapping requires from_model and to_effort")
			}
			normalized = append(normalized, mapping)
		}
		value.Accounts[accountID] = AccountConfig{Mappings: normalized}
	}
	if invalid := firstInvalidCronSchedule(value.Auto.CronSchedules); invalid != "" {
		return Config{}, fmt.Errorf("invalid model reasoning cron schedule %q", invalid)
	}
	value.Auto = normalizeAutoRoutingConfig(value.Auto)
	return value, nil
}

// TransformBody applies one account's configuration to an OpenAI JSON body.
func TransformBody(body []byte, mappings []Mapping) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	sourceEffort, effortPath := bodyEffort(body)
	mapping, matched := resolve(mappings, model, sourceEffort)
	if !matched {
		return body
	}
	result := body
	toModel := mappingTargetModel(mapping)
	if toModel != "" && toModel != model {
		if updated, err := sjson.SetBytes(result, "model", toModel); err == nil {
			result = updated
		}
	}
	effort := mappingTargetEffort(mapping)
	if effort == "" {
		return result
	}
	updated, err := sjson.SetBytes(result, effortPath, effort)
	if err != nil {
		return result
	}
	return updated
}

// ResolveUsage dispatches to the registered usage implementation.
func ResolveUsage(accountID int64, model string) UsageMapping {
	handler := usageResolver.Load().(UsageResolver)
	return handler(accountID, model)
}

// ResolveUsageWithEffort dispatches the exact usage implementation used by
// billing. The legacy ResolveUsage API remains available for integrations
// that do not have request effort metadata.
func ResolveUsageWithEffort(accountID int64, model, effort string) UsageMapping {
	handler := effortUsageResolver.Load().(EffortUsageResolver)
	return handler(accountID, model, effort)
}

// resolveUsage applies the same matching rules as TransformBody to usage
// metadata. model must be the model immediately before this plugin runs.
func resolveUsage(accountID int64, model string) UsageMapping {
	return resolveUsageWithEffort(accountID, model, "")
}

func resolveUsageWithEffort(accountID int64, model, effort string) UsageMapping {
	model = strings.TrimSpace(model)
	result := UsageMapping{Model: model}
	if accountID <= 0 || model == "" {
		return result
	}
	config, err := LoadConfig()
	if err != nil {
		return result
	}
	if len(accountMappings(config, accountID)) == 0 {
		return result
	}
	mapping, matched := resolve(accountMappings(config, accountID), model, effort)
	if !matched {
		return result
	}
	result.Matched = true
	result.Effort = mappingTargetEffort(mapping)
	if toModel := mappingTargetModel(mapping); toModel != "" && toModel != model {
		result.Model = toModel
	}
	return result
}

// UpdateUsageMappingChain replaces the pre-plugin tail with the effective
// model, preserving any channel/account mapping steps already recorded.
func UpdateUsageMappingChain(chain, requestedModel, beforeModel, afterModel string) string {
	chain = strings.TrimSpace(chain)
	requestedModel = strings.TrimSpace(requestedModel)
	beforeModel = strings.TrimSpace(beforeModel)
	afterModel = strings.TrimSpace(afterModel)
	if afterModel == "" || afterModel == beforeModel {
		return chain
	}
	if chain == "" {
		if requestedModel != "" && requestedModel != afterModel {
			return requestedModel + "→" + afterModel
		}
		return afterModel
	}
	parts := strings.Split(chain, "→")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == beforeModel {
		parts[len(parts)-1] = afterModel
		return strings.Join(parts, "→")
	}
	if strings.TrimSpace(parts[len(parts)-1]) != afterModel {
		return chain + "→" + afterModel
	}
	return chain
}

// TransformRequest is the stable runtime integration point outside this
// plugin. It dispatches through the registered implementation.
func TransformRequest(req *http.Request, accountID int64) {
	handler := requestTransformer.Load().(RequestTransformer)
	handler(req, accountID)
}

// transformRequest safely preserves the request body and updates
// ContentLength/GetBody.
func transformRequest(req *http.Request, accountID int64) {
	if req == nil || req.Body == nil || !isOpenAIRequest(req) {
		return
	}
	config, err := LoadConfig()
	if err != nil {
		return
	}
	if len(accountMappings(config, accountID)) == 0 {
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return
	}
	updated := TransformBody(body, accountMappings(config, accountID))
	if bytes.Equal(body, updated) {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(updated))
	req.ContentLength = int64(len(updated))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(updated)), nil
	}
}

func isOpenAIRequest(req *http.Request) bool {
	if req.URL == nil {
		return false
	}
	path := strings.ToLower(req.URL.Path)
	return strings.Contains(path, "/responses") || strings.Contains(path, "/chat/completions")
}

func resolve(mappings []Mapping, model, effort string) (Mapping, bool) {
	best := Mapping{}
	bestPattern := ""
	bestLen := -1
	bestIndex := -1
	requestEffort := normalize(effort)
	if strings.TrimSpace(effort) == "" {
		requestEffort = defaultReasoningEffort
	} else if requestEffort == "" {
		requestEffort = "__unknown__"
	}
	bestEffortSpecific := false
	for index, rawMapping := range mappings {
		mapping := canonicalMapping(rawMapping)
		pattern := mapping.FromModel
		if !wildcard(pattern, model) {
			if !mapping.legacy || !wildcard(mapping.ToModel, model) {
				continue
			}
			pattern = mapping.ToModel
		}
		fromEffort := normalize(mapping.FromEffort)
		effortSpecific := fromEffort != ""
		if effortSpecific && fromEffort != requestEffort {
			continue
		}
		better := len(pattern) > bestLen || (len(pattern) == bestLen && effortSpecific && !bestEffortSpecific)
		if len(pattern) == bestLen && effortSpecific == bestEffortSpecific && pattern == bestPattern {
			better = index > bestIndex
		}
		if better {
			best = mapping
			bestPattern = pattern
			bestLen = len(pattern)
			bestEffortSpecific = effortSpecific
			bestIndex = index
		}
	}
	return best, bestLen >= 0
}

func bodyEffort(body []byte) (string, string) {
	if nested := strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String()); nested != "" {
		return nested, "reasoning.effort"
	}
	if flat := strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String()); flat != "" {
		return flat, "reasoning_effort"
	}
	if gjson.GetBytes(body, "messages").Exists() {
		return defaultReasoningEffort, "reasoning_effort"
	}
	return defaultReasoningEffort, "reasoning.effort"
}

func wildcard(pattern, value string) bool {
	if pattern == gptModelPattern {
		return isGPTModel(value, true)
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern != "" && pattern == value
}

func canonicalMapping(mapping Mapping) Mapping {
	legacy := mapping.legacy || mapping.From != "" || mapping.To != "" || mapping.Effort != ""
	fromModel := strings.TrimSpace(firstNonEmpty(mapping.FromModel, mapping.From))
	fromEffort := normalize(firstNonEmpty(mapping.FromEffort, ""))
	toModel := strings.TrimSpace(firstNonEmpty(mapping.ToModel, mapping.To))
	toEffort := normalize(firstNonEmpty(mapping.ToEffort, mapping.Effort))
	return Mapping{
		FromModel:  fromModel,
		FromEffort: fromEffort,
		ToModel:    toModel,
		ToEffort:   toEffort,
		legacy:     legacy,
	}
}

func mappingTargetModel(mapping Mapping) string {
	return strings.TrimSpace(firstNonEmpty(mapping.ToModel, mapping.To))
}

func mappingTargetEffort(mapping Mapping) string {
	return normalize(firstNonEmpty(mapping.ToEffort, mapping.Effort))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func accountMappings(config Config, accountID int64) []Mapping {
	var mappings []Mapping
	auto := normalizeAutoRoutingConfig(config.Auto)
	if autoRoutingAppliesToAccount(auto, accountID) {
		autoMappings := currentAutoMappings()
		if len(autoMappings) == 0 {
			autoMappings = automaticCoverageMappings(auto.BaselineModel, defaultBaselineEffort)
		}
		mappings = append(mappings, autoMappings...)
	}
	if account, ok := config.Accounts[strconv.FormatInt(accountID, 10)]; ok {
		// Manual mappings are appended so the resolver can prefer an exact
		// manual rule over an auto-generated wildcard or tie.
		mappings = append(mappings, account.Mappings...)
	}
	return filterUnavailableTargetMappings(mappings, auto.UnavailableModels)
}

func autoRoutingAppliesToAccount(config AutoRoutingConfig, accountID int64) bool {
	if !config.Enabled || accountID <= 0 {
		return false
	}
	if len(config.AccountIDs) == 0 {
		return true
	}
	for _, selectedID := range config.AccountIDs {
		if selectedID == accountID {
			return true
		}
	}
	return false
}

func normalizePausedTargetModels(models []string) []string {
	return normalizeUnavailableModels(models)
}

func normalizeUnavailableModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(models))
	result := make([]string, 0, len(models))
	for _, model := range models {
		normalized := strings.ToLower(strings.TrimSpace(model))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil
	}
	return result
}

func unavailableModelSet(models []string) map[string]struct{} {
	models = normalizeUnavailableModels(models)
	if len(models) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(models))
	for _, model := range models {
		result[model] = struct{}{}
	}
	return result
}

func filterUnavailableTargetMappings(mappings []Mapping, unavailableModels []string) []Mapping {
	unavailable := unavailableModelSet(unavailableModels)
	if len(mappings) == 0 || len(unavailable) == 0 {
		return mappings
	}
	filtered := make([]Mapping, 0, len(mappings))
	for _, mapping := range mappings {
		targetModel := strings.ToLower(strings.TrimSpace(mappingTargetModel(mapping)))
		if targetModel != "" {
			if _, blocked := unavailable[targetModel]; blocked {
				continue
			}
		}
		filtered = append(filtered, mapping)
	}
	return filtered
}

func normalize(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
	if value == "default" {
		return defaultReasoningEffort
	}
	if _, ok := supportedEfforts[value]; !ok {
		return ""
	}
	return value
}

func configPath() string {
	if path := strings.TrimSpace(os.Getenv("SUB2API_MODEL_REASONING_EFFORT_CONFIG")); path != "" {
		return path
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, ConfigFileName)
}

func cloneConfig(value Config) Config {
	clone := Config{
		Accounts: make(map[string]AccountConfig, len(value.Accounts)),
		Auto:     value.Auto,
	}
	clone.Auto.AccountIDs = append([]int64(nil), value.Auto.AccountIDs...)
	clone.Auto.UnavailableModels = append([]string(nil), value.Auto.UnavailableModels...)
	clone.Auto.PausedTargetModels = append([]string(nil), value.Auto.PausedTargetModels...)
	clone.Auto.CronSchedules = append([]string(nil), value.Auto.CronSchedules...)
	clone.Auto.RefreshTimes = append([]string(nil), value.Auto.RefreshTimes...)
	for accountID, account := range value.Accounts {
		clone.Accounts[accountID] = AccountConfig{Mappings: append([]Mapping(nil), account.Mappings...)}
	}
	return clone
}
