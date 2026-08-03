package model_reasoning_effort

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const statsFileName = "model-reasoning-effort-stats.json"

// MappingUsageStat is intentionally separate from the host usage log. It
// describes the plugin relationship that was applied, while the host log
// remains the source-model billing record.
type MappingUsageStat struct {
	AccountID    int64     `json:"account_id"`
	FromModel    string    `json:"from_model"`
	FromEffort   string    `json:"from_effort"`
	ToModel      string    `json:"to_model"`
	ToEffort     string    `json:"to_effort"`
	Requests     int64     `json:"requests"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type mappingStatsFile struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Items     []MappingUsageStat `json:"items"`
}

var mappingStatsState struct {
	sync.Mutex
	loaded bool
	path   string
	items  map[string]MappingUsageStat
	timer  *time.Timer
}

// RecordUsageMapping records plugin-owned relationship statistics. It is
// safe to call from the normal usage path and does not mutate billing input.
func RecordUsageMapping(accountID int64, model, effort string, inputTokens, outputTokens int) {
	if accountID <= 0 {
		return
	}
	config, err := LoadConfig()
	if err != nil || !config.Auto.Enabled && len(config.Accounts) == 0 {
		return
	}
	mapping, matched := resolve(accountMappings(config, accountID), model, effort)
	if !matched {
		return
	}
	mapping = canonicalMapping(mapping)
	fromEffort := normalizeOrDefault(effort)
	stat := MappingUsageStat{
		AccountID:    accountID,
		FromModel:    strings.TrimSpace(model),
		FromEffort:   fromEffort,
		ToModel:      mappingTargetModel(mapping),
		ToEffort:     mappingTargetEffort(mapping),
		Requests:     1,
		InputTokens:  int64(maxInt(inputTokens, 0)),
		OutputTokens: int64(maxInt(outputTokens, 0)),
		LastSeenAt:   time.Now().UTC(),
	}
	key := mappingStatKey(stat)

	mappingStatsState.Lock()
	loadMappingStatsLocked()
	current := mappingStatsState.items[key]
	current.AccountID = stat.AccountID
	current.FromModel = stat.FromModel
	current.FromEffort = stat.FromEffort
	current.ToModel = stat.ToModel
	current.ToEffort = stat.ToEffort
	current.Requests += stat.Requests
	current.InputTokens += stat.InputTokens
	current.OutputTokens += stat.OutputTokens
	current.LastSeenAt = stat.LastSeenAt
	mappingStatsState.items[key] = current
	scheduleMappingStatsFlushLocked()
	mappingStatsState.Unlock()
}

// RecordUsageMappingFromResult keeps the host usage hook to one primitive call
// while handling the optional effort field inside the plugin boundary.
func RecordUsageMappingFromResult(accountID int64, model string, effort *string, inputTokens, outputTokens int) {
	sourceEffort := ""
	if effort != nil {
		sourceEffort = *effort
	}
	RecordUsageMapping(accountID, model, sourceEffort, inputTokens, outputTokens)
}

// LoadMappingUsageStats returns plugin-owned statistics as a detached sorted
// slice. It does not read or merge the host usage-log tables.
func LoadMappingUsageStats() ([]MappingUsageStat, error) {
	mappingStatsState.Lock()
	defer mappingStatsState.Unlock()
	if err := loadMappingStatsLocked(); err != nil {
		return nil, err
	}
	items := make([]MappingUsageStat, 0, len(mappingStatsState.items))
	for _, item := range mappingStatsState.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AccountID != items[j].AccountID {
			return items[i].AccountID < items[j].AccountID
		}
		return mappingStatKey(items[i]) < mappingStatKey(items[j])
	})
	return items, nil
}

func mappingStatKey(stat MappingUsageStat) string {
	return strconv.FormatInt(stat.AccountID, 10) + "|" + stat.FromModel + "@" + stat.FromEffort + "->" + stat.ToModel + "@" + stat.ToEffort
}

func loadMappingStatsLocked() error {
	path := statsPath()
	if mappingStatsState.loaded && mappingStatsState.path == path {
		return nil
	}
	mappingStatsState.loaded = true
	mappingStatsState.path = path
	mappingStatsState.items = make(map[string]MappingUsageStat)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file mappingStatsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	for _, item := range file.Items {
		item.FromEffort = normalizeOrDefault(item.FromEffort)
		item.ToEffort = normalizeOrDefault(item.ToEffort)
		mappingStatsState.items[mappingStatKey(item)] = item
	}
	return nil
}

func normalizeOrDefault(effort string) string {
	if normalized := normalize(effort); normalized != "" {
		return normalized
	}
	return defaultReasoningEffort
}

func scheduleMappingStatsFlushLocked() {
	if mappingStatsState.timer != nil {
		return
	}
	mappingStatsState.timer = time.AfterFunc(time.Second, func() {
		_ = flushMappingStats()
	})
}

func flushMappingStats() error {
	mappingStatsState.Lock()
	defer mappingStatsState.Unlock()
	if mappingStatsState.timer != nil {
		mappingStatsState.timer = nil
	}
	if err := loadMappingStatsLocked(); err != nil {
		return err
	}
	items := make([]MappingUsageStat, 0, len(mappingStatsState.items))
	for _, item := range mappingStatsState.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return mappingStatKey(items[i]) < mappingStatKey(items[j]) })
	data, err := json.MarshalIndent(mappingStatsFile{UpdatedAt: time.Now().UTC(), Items: items}, "", "  ")
	if err != nil {
		return err
	}
	path := mappingStatsState.path
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".model-reasoning-effort-stats-*.tmp")
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
	return os.Rename(tmpPath, path)
}

func statsPath() string {
	if path := strings.TrimSpace(os.Getenv("SUB2API_MODEL_REASONING_EFFORT_STATS")); path != "" {
		return path
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, statsFileName)
}

func maxInt(value, fallback int) int {
	if value < fallback {
		return fallback
	}
	return value
}
