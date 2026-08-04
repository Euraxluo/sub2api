package billing_strategy

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxAuditRecords = 500
	auditFileName   = "billing-strategy-audits.json"
)

// Candidate is the unmultiplied cost evaluation for one model in a request's
// billing chain. It is intentionally separate from the host usage log so the
// normal usage UI does not need to expose billing-strategy internals.
type Candidate struct {
	Model           string  `json:"model"`
	PricingSource   string  `json:"pricing_source"`
	Available       bool    `json:"available"`
	Error           string  `json:"error,omitempty"`
	BillingMode     string  `json:"billing_mode,omitempty"`
	InputCost       float64 `json:"input_cost"`
	ImageInputCost  float64 `json:"image_input_cost"`
	OutputCost      float64 `json:"output_cost"`
	ImageOutputCost float64 `json:"image_output_cost"`
	CacheWriteCost  float64 `json:"cache_write_cost"`
	CacheReadCost   float64 `json:"cache_read_cost"`
	TotalCost       float64 `json:"total_cost"`
}

// AuditRecord is one recent max-cost billing decision. It contains no prompt
// or credential data and is only exposed through the administrator tool.
type AuditRecord struct {
	RequestID             string      `json:"request_id,omitempty"`
	AccountID             int64       `json:"account_id"`
	RequestedModel        string      `json:"requested_model"`
	MappingChain          string      `json:"mapping_chain,omitempty"`
	Strategy              string      `json:"strategy"`
	SelectedModel         string      `json:"selected_model"`
	InputTokens           int         `json:"input_tokens"`
	OutputTokens          int         `json:"output_tokens"`
	CacheCreationTokens   int         `json:"cache_creation_tokens"`
	CacheReadTokens       int         `json:"cache_read_tokens"`
	ImageInputTokens      int         `json:"image_input_tokens"`
	ImageOutputTokens     int         `json:"image_output_tokens"`
	ImageCount            int         `json:"image_count"`
	GroupRateMultiplier   float64     `json:"group_rate_multiplier"`
	AccountRateMultiplier float64     `json:"account_rate_multiplier"`
	TotalCost             float64     `json:"total_cost"`
	ActualCost            float64     `json:"actual_cost"`
	AccountStatsCost      *float64    `json:"account_stats_cost,omitempty"`
	AccountBilledCost     float64     `json:"account_billed_cost"`
	Candidates            []Candidate `json:"candidates"`
	CreatedAt             time.Time   `json:"created_at"`
}

type auditFile struct {
	UpdatedAt time.Time     `json:"updated_at"`
	Items     []AuditRecord `json:"items"`
}

// SelectMostExpensive selects the available candidate with the greatest raw
// TotalCost. Multipliers are deliberately absent from this comparison; the
// host applies the effective user/group multiplier after the strategy result
// has been selected.
func SelectMostExpensive(candidates []Candidate) (int, bool) {
	best := -1
	for index, candidate := range candidates {
		if !candidate.Available || validCost(candidate.TotalCost) != candidate.TotalCost {
			continue
		}
		if best < 0 || candidate.TotalCost > candidates[best].TotalCost {
			best = index
		}
	}
	return best, best >= 0
}

var auditState struct {
	sync.Mutex
	loaded bool
	path   string
	items  []AuditRecord
	timer  *time.Timer
}

// Record stores the newest billing decision and keeps the ledger bounded.
func Record(record AuditRecord) {
	if record.AccountID <= 0 || strings.TrimSpace(record.SelectedModel) == "" {
		return
	}
	record.RequestedModel = strings.TrimSpace(record.RequestedModel)
	record.SelectedModel = strings.TrimSpace(record.SelectedModel)
	record.Strategy = strings.TrimSpace(record.Strategy)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	record.Candidates = cloneCandidates(record.Candidates)

	auditState.Lock()
	defer auditState.Unlock()
	if err := loadLocked(); err != nil {
		return
	}
	auditState.items = append([]AuditRecord{record}, auditState.items...)
	if len(auditState.items) > maxAuditRecords {
		auditState.items = auditState.items[:maxAuditRecords]
	}
	scheduleFlushLocked()
}

// List returns newest-first records, optionally limited to one account.
func List(accountID int64, limit int) ([]AuditRecord, error) {
	if limit <= 0 || limit > maxAuditRecords {
		limit = 100
	}
	auditState.Lock()
	defer auditState.Unlock()
	if err := loadLocked(); err != nil {
		return nil, err
	}
	items := make([]AuditRecord, 0, minInt(limit, len(auditState.items)))
	for _, item := range auditState.items {
		if accountID > 0 && item.AccountID != accountID {
			continue
		}
		items = append(items, cloneRecord(item))
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func cloneRecord(record AuditRecord) AuditRecord {
	record.Candidates = cloneCandidates(record.Candidates)
	return record
}

func cloneCandidates(candidates []Candidate) []Candidate {
	if len(candidates) == 0 {
		return []Candidate{}
	}
	return append([]Candidate(nil), candidates...)
}

func loadLocked() error {
	path := auditPath()
	if auditState.loaded && auditState.path == path {
		return nil
	}
	auditState.loaded = true
	auditState.path = path
	auditState.items = nil
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file auditFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	for _, item := range file.Items {
		if item.AccountID <= 0 || strings.TrimSpace(item.SelectedModel) == "" {
			continue
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.Candidates = cloneCandidates(item.Candidates)
		auditState.items = append(auditState.items, item)
		if len(auditState.items) >= maxAuditRecords {
			break
		}
	}
	return nil
}

func scheduleFlushLocked() {
	if auditState.timer != nil {
		return
	}
	auditState.timer = time.AfterFunc(time.Second, func() {
		_ = flush()
	})
}

func flush() error {
	auditState.Lock()
	defer auditState.Unlock()
	if auditState.timer != nil {
		auditState.timer = nil
	}
	if err := loadLocked(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(auditFile{
		UpdatedAt: time.Now().UTC(),
		Items:     auditState.items,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := auditState.path
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".billing-strategy-audits-*.tmp")
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

func auditPath() string {
	if path := strings.TrimSpace(os.Getenv("SUB2API_BILLING_STRATEGY_AUDIT")); path != "" {
		return path
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, auditFileName)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validCost(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return value
}
