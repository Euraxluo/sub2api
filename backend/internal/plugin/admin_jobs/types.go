package admin_jobs

import (
	"context"
	"sort"
	"time"
)

const (
	TaskKindBuiltin    = "builtin"
	TaskKindJavaScript = "javascript"

	NotifyNever           = "never"
	NotifyFailure         = "failure"
	NotifyFailureRecovery = "failure_recovery"
	NotifyAlways          = "always"

	StatusOK       = "ok"
	StatusWarning  = "warning"
	StatusCritical = "critical"
	StatusError    = "error"

	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"
)

// FieldDefinition describes one configurable field exposed by a built-in job.
// Secret fields are accepted on write but never returned by task APIs.
type FieldDefinition struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Default     any    `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

// BuiltinDefinition is the Admin Tools metadata for a compiled Go job.
type BuiltinDefinition struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Fields      []FieldDefinition `json:"fields,omitempty"`
}

// Task is the persisted job definition and its latest runtime state.
type Task struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	BuiltinID   string `json:"builtin_id,omitempty"`
	Script      string `json:"script,omitempty"`

	Input   map[string]any    `json:"input,omitempty"`
	Secrets map[string]string `json:"secrets,omitempty"`

	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	NotifyPolicy    string `json:"notify_policy"`
	NotifyManual    bool   `json:"notify_manual"`
	CooldownSeconds int    `json:"cooldown_seconds"`

	LastStatus          string     `json:"last_status,omitempty"`
	LastMessage         string     `json:"last_message,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastNotificationAt  *time.Time `json:"last_notification_at,omitempty"`
	LastNotificationKey string     `json:"last_notification_key,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskView removes secret values while retaining their configured field names.
type TaskView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Kind        string         `json:"kind"`
	BuiltinID   string         `json:"builtin_id,omitempty"`
	Script      string         `json:"script,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	SecretKeys  []string       `json:"secret_keys,omitempty"`

	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"interval_seconds"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	NotifyPolicy    string `json:"notify_policy"`
	NotifyManual    bool   `json:"notify_manual"`
	CooldownSeconds int    `json:"cooldown_seconds"`

	Running            bool       `json:"running"`
	LastStatus         string     `json:"last_status,omitempty"`
	LastMessage        string     `json:"last_message,omitempty"`
	LastRunAt          *time.Time `json:"last_run_at,omitempty"`
	NextRunAt          *time.Time `json:"next_run_at,omitempty"`
	LastNotificationAt *time.Time `json:"last_notification_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func taskView(task *Task, running bool) TaskView {
	secretKeys := make([]string, 0, len(task.Secrets))
	for key, value := range task.Secrets {
		if value != "" {
			secretKeys = append(secretKeys, key)
		}
	}
	sort.Strings(secretKeys)
	return TaskView{
		ID: task.ID, Name: task.Name, Description: task.Description,
		Kind: task.Kind, BuiltinID: task.BuiltinID, Script: task.Script,
		Input: cloneAnyMap(task.Input), SecretKeys: secretKeys,
		Enabled: task.Enabled, IntervalSeconds: task.IntervalSeconds,
		TimeoutSeconds: task.TimeoutSeconds, NotifyPolicy: task.NotifyPolicy,
		NotifyManual: task.NotifyManual, CooldownSeconds: task.CooldownSeconds,
		Running: running, LastStatus: task.LastStatus, LastMessage: task.LastMessage,
		LastRunAt: cloneTimePtr(task.LastRunAt), NextRunAt: cloneTimePtr(task.NextRunAt),
		LastNotificationAt: cloneTimePtr(task.LastNotificationAt),
		CreatedAt:          task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

// TaskDraft is accepted by create/update APIs. Empty secret values preserve
// existing values on update; ClearSecretKeys explicitly removes them.
type TaskDraft struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Kind        string            `json:"kind"`
	BuiltinID   string            `json:"builtin_id,omitempty"`
	Script      string            `json:"script,omitempty"`
	Input       map[string]any    `json:"input,omitempty"`
	Secrets     map[string]string `json:"secrets,omitempty"`

	ClearSecretKeys []string `json:"clear_secret_keys,omitempty"`
	Enabled         bool     `json:"enabled"`
	IntervalSeconds int      `json:"interval_seconds"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	NotifyPolicy    string   `json:"notify_policy"`
	NotifyManual    bool     `json:"notify_manual"`
	CooldownSeconds int      `json:"cooldown_seconds"`
}

// RunPayload is supplied to compiled and JavaScript jobs.
type RunPayload struct {
	TaskID   string            `json:"task_id"`
	TaskName string            `json:"task_name"`
	Input    map[string]any    `json:"input"`
	Secrets  map[string]string `json:"secrets"`
}

// Result is the common result protocol for all jobs.
type Result struct {
	Status   string         `json:"status"`
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message,omitempty"`
	DedupKey string         `json:"dedup_key,omitempty"`
	Data     map[string]any `json:"data,omitempty"`

	// secretUpdates lets a built-in rotate credentials without exposing them in
	// API responses or run history. Only the manager persists these values.
	secretUpdates map[string]string
}

// RunRecord is retained for Admin Tools history and troubleshooting.
type RunRecord struct {
	ID          string         `json:"id"`
	TaskID      string         `json:"task_id"`
	TaskName    string         `json:"task_name"`
	Trigger     string         `json:"trigger"`
	Status      string         `json:"status"`
	Title       string         `json:"title,omitempty"`
	Message     string         `json:"message,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	Stdout      string         `json:"stdout,omitempty"`
	Stderr      string         `json:"stderr,omitempty"`
	Error       string         `json:"error,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	FinishedAt  time.Time      `json:"finished_at"`
	DurationMS  int64          `json:"duration_ms"`
	Notified    bool           `json:"notified"`
	NotifyError string         `json:"notify_error,omitempty"`
}

// Notification is handed to the existing Admin Tools notification provider.
type Notification struct {
	TaskID   string
	TaskName string
	Severity string
	Title    string
	Body     string
	DedupKey string
}

type Notifier func(context.Context, Notification) error
type Handler func(context.Context, RunPayload) (Result, error)

type executionOutput struct {
	Result Result
	Stdout string
	Stderr string
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
