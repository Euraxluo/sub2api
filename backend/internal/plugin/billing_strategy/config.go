package billing_strategy

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	configFileName       = "billing-strategy-config.json"
	maxAccountMultiplier = 10000.0
)

var ErrInvalidAccountMultiplier = errors.New("invalid account multiplier")

type Config struct {
	AccountMultipliers map[string]float64 `json:"account_multipliers"`
}

type resolvedAccountMultiplier struct {
	base      float64
	factor    float64
	effective float64
}

var configState struct {
	sync.Mutex
	loaded bool
	path   string
	value  Config
}

func LoadConfig() (Config, error) {
	configState.Lock()
	defer configState.Unlock()
	if err := loadConfigLocked(); err != nil {
		return Config{}, err
	}
	return cloneConfig(configState.value), nil
}

func AccountRateMultiplierFactor(accountID int64) float64 {
	factor, err := configuredAccountRateMultiplierFactor(accountID)
	if err != nil {
		return 1
	}
	return factor
}

func configuredAccountRateMultiplierFactor(accountID int64) (float64, error) {
	if accountID <= 0 {
		return 1, nil
	}
	config, err := LoadConfig()
	if err != nil {
		return 0, err
	}
	if factor, ok := config.AccountMultipliers[strconv.FormatInt(accountID, 10)]; ok {
		return factor, nil
	}
	return 1, nil
}

// CalculateVirtualModelCost converts the raw winning-model cost into the
// plugin-owned virtual model cost. The host then applies its existing user or
// group rate to this value without changing the normal billing formula.
func CalculateVirtualModelCost(accountID int64, rawCost, accountRateMultiplier float64) float64 {
	multiplier := resolveAccountMultiplier(accountID, accountRateMultiplier)
	return validCost(rawCost) * multiplier.effective
}

// ApplyVirtualBillingCost scales host-owned cost components to the virtual
// total while leaving the host's concrete cost type inside the host package.
func ApplyVirtualBillingCost(rawTotal, virtualTotal float64, totalCost, actualCost *float64, components ...*float64) {
	rawTotal = validCost(rawTotal)
	virtualTotal = validCost(virtualTotal)
	scale := 0.0
	if rawTotal > 0 {
		scale = virtualTotal / rawTotal
	}
	for _, component := range components {
		if component != nil {
			*component *= scale
		}
	}
	if totalCost != nil {
		*totalCost = virtualTotal
	}
	if actualCost != nil {
		*actualCost = virtualTotal
	}
}

func resolveAccountMultiplier(accountID int64, base float64) resolvedAccountMultiplier {
	if base < 0 || math.IsNaN(base) || math.IsInf(base, 0) {
		base = 1
	}
	factor := AccountRateMultiplierFactor(accountID)
	return resolveAccountMultiplierWithFactor(base, factor)
}

func resolveAccountMultiplierWithFactor(base, factor float64) resolvedAccountMultiplier {
	effective := base * factor
	if effective < 0 || math.IsNaN(effective) || math.IsInf(effective, 0) {
		factor = 1
		effective = base
	}
	return resolvedAccountMultiplier{base: base, factor: factor, effective: effective}
}

func SetAccountRateMultiplierFactor(accountIDs []int64, factor float64) (Config, error) {
	if len(accountIDs) == 0 || !validAccountMultiplier(factor) {
		return Config{}, ErrInvalidAccountMultiplier
	}
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			return Config{}, ErrInvalidAccountMultiplier
		}
	}

	configState.Lock()
	defer configState.Unlock()
	if err := loadConfigLocked(); err != nil {
		return Config{}, err
	}
	next := cloneConfig(configState.value)
	for _, accountID := range accountIDs {
		key := strconv.FormatInt(accountID, 10)
		if factor == 1 {
			delete(next.AccountMultipliers, key)
		} else {
			next.AccountMultipliers[key] = factor
		}
	}
	if err := writeConfig(configState.path, next); err != nil {
		return Config{}, err
	}
	configState.value = next
	return cloneConfig(next), nil
}

func validAccountMultiplier(value float64) bool {
	return value > 0 && value <= maxAccountMultiplier && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func loadConfigLocked() error {
	path := configPath()
	if configState.loaded && configState.path == path {
		return nil
	}
	value := Config{AccountMultipliers: map[string]float64{}}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	value = normalizeConfig(value)
	configState.loaded = true
	configState.path = path
	configState.value = value
	return nil
}

func normalizeConfig(value Config) Config {
	normalized := Config{AccountMultipliers: make(map[string]float64, len(value.AccountMultipliers))}
	for rawID, factor := range value.AccountMultipliers {
		accountID, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
		if err != nil || accountID <= 0 || !validAccountMultiplier(factor) || factor == 1 {
			continue
		}
		normalized.AccountMultipliers[strconv.FormatInt(accountID, 10)] = factor
	}
	return normalized
}

func cloneConfig(value Config) Config {
	clone := Config{AccountMultipliers: make(map[string]float64, len(value.AccountMultipliers))}
	for accountID, factor := range value.AccountMultipliers {
		clone.AccountMultipliers[accountID] = factor
	}
	return clone
}

func writeConfig(path string, value Config) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".billing-strategy-config-*.tmp")
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

func configPath() string {
	if path := strings.TrimSpace(os.Getenv("SUB2API_BILLING_STRATEGY_CONFIG")); path != "" {
		return path
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, configFileName)
}
