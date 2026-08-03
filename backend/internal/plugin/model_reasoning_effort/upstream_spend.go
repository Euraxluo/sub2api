package model_reasoning_effort

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	upstreamSpendFileName   = "model-reasoning-effort-upstream-spend.json"
	upstreamSpendBucketSize = 5 * time.Minute
	upstreamSpendRetention  = 8 * 24 * time.Hour
)

type upstreamSpendFile struct {
	UpdatedAt time.Time                      `json:"updated_at"`
	Buckets   map[string]upstreamUsageBucket `json:"buckets"`
}

// UpstreamUsageTotals is the provider-side usage accumulated for one account
// in a calendar window. It deliberately excludes user billing multipliers.
type UpstreamUsageTotals struct {
	CostUSD float64
	Tokens  int64
}

type upstreamUsageBucket struct {
	CostUSD float64 `json:"cost_usd,omitempty"`
	Tokens  int64   `json:"tokens,omitempty"`
}

// UnmarshalJSON keeps the ledger compatible with the first implementation,
// where each bucket was stored as a plain cost number.
func (bucket *upstreamUsageBucket) UnmarshalJSON(data []byte) error {
	var legacyCost float64
	if err := json.Unmarshal(data, &legacyCost); err == nil {
		bucket.CostUSD = legacyCost
		bucket.Tokens = 0
		return nil
	}
	type wire upstreamUsageBucket
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*bucket = upstreamUsageBucket(decoded)
	return nil
}

var upstreamSpendState struct {
	sync.Mutex
	loaded bool
	path   string
	bucket map[string]upstreamUsageBucket
	timer  *time.Timer
}

// RecordUpstreamUsage records provider-side standard cost and total token usage
// for the upstream account. Either value may be zero; the bucket is ignored
// only when both values are empty.
func RecordUpstreamUsage(accountID int64, costUSD float64, totalTokens int64) {
	if accountID <= 0 {
		return
	}
	if math.IsNaN(costUSD) || math.IsInf(costUSD, 0) || costUSD < 0 {
		costUSD = 0
	}
	if totalTokens < 0 {
		totalTokens = 0
	}
	if costUSD == 0 && totalTokens == 0 {
		return
	}
	now := time.Now().UTC()

	upstreamSpendState.Lock()
	defer upstreamSpendState.Unlock()
	if err := loadUpstreamSpendLocked(); err != nil {
		return
	}
	pruneUpstreamSpendLocked(now)
	key := upstreamSpendBucketKey(accountID, now)
	current := upstreamSpendState.bucket[key]
	current.CostUSD += costUSD
	current.Tokens = saturatingAddInt64(current.Tokens, totalTokens)
	upstreamSpendState.bucket[key] = current
	scheduleUpstreamSpendFlushLocked()
}

// RecordUpstreamCost is kept as a compatibility wrapper for callers that only
// have provider-side cost available.
func RecordUpstreamCost(accountID int64, costUSD float64) {
	RecordUpstreamUsage(accountID, costUSD, 0)
}

// DailyUpstreamUsage returns provider-side cost and total tokens for one
// account since the start of the current calendar day in timezoneName.
func DailyUpstreamUsage(accountID int64, timezoneName string, now time.Time) UpstreamUsageTotals {
	if accountID <= 0 {
		return UpstreamUsageTotals{}
	}
	location := time.UTC
	if name := strings.TrimSpace(timezoneName); name != "" {
		if loaded, err := time.LoadLocation(name); err == nil {
			location = loaded
		}
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	return upstreamUsageBetween(accountID, start.UTC(), now.UTC())
}

// WeeklyUpstreamUsage returns provider-side cost and total tokens for one
// account since Monday 00:00 in timezoneName. A week is a local calendar
// week, so daylight-saving transitions do not shift the boundary.
func WeeklyUpstreamUsage(accountID int64, timezoneName string, now time.Time) UpstreamUsageTotals {
	if accountID <= 0 {
		return UpstreamUsageTotals{}
	}
	location := time.UTC
	if name := strings.TrimSpace(timezoneName); name != "" {
		if loaded, err := time.LoadLocation(name); err == nil {
			location = loaded
		}
	}
	localNow := now.In(location)
	daysSinceMonday := (int(localNow.Weekday()) + 6) % 7
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day()-daysSinceMonday, 0, 0, 0, 0, location)
	return upstreamUsageBetween(accountID, start.UTC(), now.UTC())
}

func upstreamUsageBetween(accountID int64, start, end time.Time) UpstreamUsageTotals {
	if accountID <= 0 {
		return UpstreamUsageTotals{}
	}
	start = start.UTC()
	end = end.UTC()
	if end.Before(start) {
		return UpstreamUsageTotals{}
	}

	upstreamSpendState.Lock()
	defer upstreamSpendState.Unlock()
	if err := loadUpstreamSpendLocked(); err != nil {
		return UpstreamUsageTotals{}
	}

	firstBucket := start.UTC().Truncate(upstreamSpendBucketSize)
	lastBucket := end.Truncate(upstreamSpendBucketSize)
	if lastBucket.Before(firstBucket) {
		return UpstreamUsageTotals{}
	}
	totals := UpstreamUsageTotals{}
	prefix := strconv.FormatInt(accountID, 10) + "|"
	for key, bucketUsage := range upstreamSpendState.bucket {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		bucketUnix, err := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64)
		if err != nil {
			continue
		}
		bucket := time.Unix(bucketUnix, 0).UTC()
		if !bucket.Before(firstBucket) && !bucket.After(lastBucket) {
			totals.CostUSD += bucketUsage.CostUSD
			totals.Tokens = saturatingAddInt64(totals.Tokens, bucketUsage.Tokens)
		}
	}
	return totals
}

// DailyUpstreamCost returns provider-side cost for compatibility with the
// original plugin facade.
func DailyUpstreamCost(accountID int64, timezoneName string, now time.Time) float64 {
	return DailyUpstreamUsage(accountID, timezoneName, now).CostUSD
}

// DailyUpstreamTokens returns provider-side total tokens for the current day.
func DailyUpstreamTokens(accountID int64, timezoneName string, now time.Time) int64 {
	return DailyUpstreamUsage(accountID, timezoneName, now).Tokens
}

func upstreamSpendBucketKey(accountID int64, at time.Time) string {
	return strconv.FormatInt(accountID, 10) + "|" + strconv.FormatInt(at.UTC().Truncate(upstreamSpendBucketSize).Unix(), 10)
}

func loadUpstreamSpendLocked() error {
	path := upstreamSpendPath()
	if upstreamSpendState.loaded && upstreamSpendState.path == path {
		return nil
	}
	bucket := make(map[string]upstreamUsageBucket)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		upstreamSpendState.loaded = true
		upstreamSpendState.path = path
		upstreamSpendState.bucket = bucket
		return nil
	}
	if err != nil {
		return err
	}
	var file upstreamSpendFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	for key, usage := range file.Buckets {
		if usage.CostUSD < 0 || math.IsNaN(usage.CostUSD) || math.IsInf(usage.CostUSD, 0) {
			usage.CostUSD = 0
		}
		if usage.Tokens < 0 {
			usage.Tokens = 0
		}
		if usage.CostUSD > 0 || usage.Tokens > 0 {
			bucket[key] = usage
		}
	}
	upstreamSpendState.loaded = true
	upstreamSpendState.path = path
	upstreamSpendState.bucket = bucket
	return nil
}

func pruneUpstreamSpendLocked(now time.Time) {
	cutoff := now.Add(-upstreamSpendRetention).Truncate(upstreamSpendBucketSize).Unix()
	for key := range upstreamSpendState.bucket {
		separator := strings.LastIndexByte(key, '|')
		if separator < 0 {
			delete(upstreamSpendState.bucket, key)
			continue
		}
		bucketUnix, err := strconv.ParseInt(key[separator+1:], 10, 64)
		usage := upstreamSpendState.bucket[key]
		if err != nil || bucketUnix < cutoff || (usage.CostUSD <= 0 && usage.Tokens <= 0) {
			delete(upstreamSpendState.bucket, key)
		}
	}
}

func scheduleUpstreamSpendFlushLocked() {
	if upstreamSpendState.timer != nil {
		return
	}
	upstreamSpendState.timer = time.AfterFunc(time.Second, func() {
		_ = flushUpstreamSpend()
	})
}

func flushUpstreamSpend() error {
	upstreamSpendState.Lock()
	defer upstreamSpendState.Unlock()
	if upstreamSpendState.timer != nil {
		upstreamSpendState.timer = nil
	}
	if err := loadUpstreamSpendLocked(); err != nil {
		return err
	}
	pruneUpstreamSpendLocked(time.Now().UTC())
	data, err := json.MarshalIndent(upstreamSpendFile{
		UpdatedAt: time.Now().UTC(),
		Buckets:   upstreamSpendState.bucket,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := upstreamSpendState.path
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".model-reasoning-effort-upstream-spend-*.tmp")
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

func upstreamSpendPath() string {
	if path := strings.TrimSpace(os.Getenv("SUB2API_MODEL_REASONING_EFFORT_SPEND")); path != "" {
		return path
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "/app/data"
	}
	return filepath.Join(dataDir, upstreamSpendFileName)
}

func saturatingAddInt64(left, right int64) int64 {
	if right <= 0 {
		return left
	}
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}
