package admin_jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultIntervalSeconds = 10 * 60
	defaultTimeoutSeconds  = 30
	defaultCooldownSeconds = 30 * 60
	minIntervalSeconds     = 30
	maxIntervalSeconds     = 7 * 24 * 60 * 60
	maxTimeoutSeconds      = 5 * 60
	maxTaskTextBytes       = 128 * 1024
	maxRunHistory          = 200
	schedulerTick          = 5 * time.Second
	maxConcurrentRuns      = 4
)

var (
	ErrTaskNotFound = errors.New("admin job not found")
	ErrTaskRunning  = errors.New("admin job is already running")
)

var notifierRegistry = struct {
	sync.RWMutex
	notifier Notifier
}{}

// RegisterNotifier connects jobs to the notification provider selected in
// Admin Tools. The host only supplies this adapter; all notification policy
// remains plugin-owned.
func RegisterNotifier(notifier Notifier) {
	notifierRegistry.Lock()
	notifierRegistry.notifier = notifier
	notifierRegistry.Unlock()

	defaultManagerMu.Lock()
	if defaultManagerInstance != nil {
		defaultManagerInstance.SetNotifier(notifier)
	}
	defaultManagerMu.Unlock()
}

func currentNotifier() Notifier {
	notifierRegistry.RLock()
	defer notifierRegistry.RUnlock()
	return notifierRegistry.notifier
}

type Manager struct {
	mu sync.Mutex

	store   fileStore
	tasks   map[string]*Task
	runs    []RunRecord
	running map[string]struct{}
	initErr error

	notifier Notifier
	now      func() time.Time
	sem      chan struct{}
	wakeCh   chan struct{}
	stopCh   chan struct{}
	started  bool
	stopped  bool
	wg       sync.WaitGroup
}

var (
	defaultManagerMu       sync.Mutex
	defaultManagerInstance *Manager
)

// DefaultManager returns the process-wide Admin Jobs manager.
func DefaultManager() *Manager {
	defaultManagerMu.Lock()
	defer defaultManagerMu.Unlock()
	if defaultManagerInstance != nil {
		return defaultManagerInstance
	}
	manager, err := NewManager(defaultStorePath())
	if err != nil {
		manager = newEmptyManager(defaultStorePath())
		manager.initErr = err
	}
	manager.notifier = currentNotifier()
	defaultManagerInstance = manager
	return manager
}

func newEmptyManager(storePath string) *Manager {
	return &Manager{
		store:   fileStore{path: storePath},
		tasks:   make(map[string]*Task),
		runs:    []RunRecord{},
		running: make(map[string]struct{}),
		now:     time.Now,
		sem:     make(chan struct{}, maxConcurrentRuns),
		wakeCh:  make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
	}
}

// NewManager loads a manager from a plugin-owned state file.
func NewManager(storePath string) (*Manager, error) {
	manager := newEmptyManager(storePath)
	state, err := manager.store.load()
	if err != nil {
		return nil, err
	}
	for _, task := range state.Tasks {
		if task == nil || strings.TrimSpace(task.ID) == "" {
			continue
		}
		task.Input = cloneAnyMap(task.Input)
		task.Secrets = cloneStringMap(task.Secrets)
		manager.tasks[task.ID] = task
	}
	manager.runs = state.Runs
	if len(manager.runs) > maxRunHistory {
		manager.runs = manager.runs[:maxRunHistory]
	}
	manager.notifier = currentNotifier()
	return manager, nil
}

func (m *Manager) SetNotifier(notifier Notifier) {
	m.mu.Lock()
	m.notifier = notifier
	m.mu.Unlock()
}

// Start enables periodic execution. It is idempotent and does not write state.
func (m *Manager) Start() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.started || m.stopped {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.wg.Add(1)
	m.mu.Unlock()
	go m.schedulerLoop()
}

// Stop is primarily used by tests and graceful shutdown hooks.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopCh)
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) schedulerLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(schedulerTick)
	defer ticker.Stop()
	m.runDue()
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.wakeCh:
			m.runDue()
		case <-ticker.C:
			m.runDue()
		}
	}
}

func (m *Manager) signalWake() {
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

func (m *Manager) runDue() {
	now := m.now().UTC()
	m.mu.Lock()
	if m.initErr != nil {
		m.mu.Unlock()
		return
	}
	dueIDs := make([]string, 0)
	stateChanged := false
	for id, task := range m.tasks {
		if !task.Enabled || task.IntervalSeconds < minIntervalSeconds {
			continue
		}
		if _, isRunning := m.running[id]; isRunning {
			continue
		}
		if task.NextRunAt == nil {
			next := now.Add(time.Duration(task.IntervalSeconds) * time.Second)
			task.NextRunAt = &next
			stateChanged = true
			continue
		}
		if !task.NextRunAt.After(now) {
			dueIDs = append(dueIDs, id)
		}
	}
	if stateChanged {
		if err := m.persistLocked(); err != nil {
			slog.Error("admin jobs: persist next run state failed", "error", err)
		}
	}
	m.mu.Unlock()

	for _, id := range dueIDs {
		task, err := m.reserveTask(id)
		if err != nil {
			continue
		}
		go func(snapshot *Task) {
			_, _ = m.executeReserved(context.Background(), snapshot, TriggerScheduled, false)
		}(task)
	}
}

func (m *Manager) ListTasks() ([]TaskView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, m.initErr
	}
	views := make([]TaskView, 0, len(m.tasks))
	for id, task := range m.tasks {
		_, running := m.running[id]
		views = append(views, taskView(task, running))
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].UpdatedAt.Equal(views[j].UpdatedAt) {
			return views[i].ID < views[j].ID
		}
		return views[i].UpdatedAt.After(views[j].UpdatedAt)
	})
	return views, nil
}

func (m *Manager) GetTask(id string) (TaskView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return TaskView{}, m.initErr
	}
	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return TaskView{}, ErrTaskNotFound
	}
	_, running := m.running[task.ID]
	return taskView(task, running), nil
}

func (m *Manager) CreateTask(draft TaskDraft) (TaskView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return TaskView{}, m.initErr
	}
	now := m.now().UTC()
	task := &Task{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now, Input: map[string]any{}, Secrets: map[string]string{}}
	if err := applyDraft(task, draft, false, now); err != nil {
		return TaskView{}, err
	}
	m.tasks[task.ID] = task
	if err := m.persistLocked(); err != nil {
		delete(m.tasks, task.ID)
		return TaskView{}, err
	}
	view := taskView(task, false)
	go m.signalWake()
	return view, nil
}

func (m *Manager) UpdateTask(id string, draft TaskDraft) (TaskView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return TaskView{}, m.initErr
	}
	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return TaskView{}, ErrTaskNotFound
	}
	previous := cloneTask(task)
	if err := applyDraft(task, draft, true, m.now().UTC()); err != nil {
		return TaskView{}, err
	}
	if err := m.persistLocked(); err != nil {
		m.tasks[task.ID] = previous
		return TaskView{}, err
	}
	_, running := m.running[task.ID]
	view := taskView(task, running)
	go m.signalWake()
	return view, nil
}

func (m *Manager) DeleteTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return m.initErr
	}
	id = strings.TrimSpace(id)
	task, ok := m.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if _, running := m.running[id]; running {
		return ErrTaskRunning
	}
	delete(m.tasks, id)
	if err := m.persistLocked(); err != nil {
		m.tasks[id] = task
		return err
	}
	return nil
}

func (m *Manager) ListRuns(taskID string, limit int) ([]RunRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, m.initErr
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	taskID = strings.TrimSpace(taskID)
	result := make([]RunRecord, 0, limit)
	for _, run := range m.runs {
		if taskID != "" && run.TaskID != taskID {
			continue
		}
		run.Data = cloneAnyMap(run.Data)
		result = append(result, run)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

// Run executes one task synchronously. Manual callers may force one test
// notification without changing the saved policy.
func (m *Manager) Run(ctx context.Context, id, trigger string, forceNotify bool) (RunRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := m.reserveTask(id)
	if err != nil {
		return RunRecord{}, err
	}
	return m.executeReserved(ctx, task, trigger, forceNotify)
}

func (m *Manager) reserveTask(id string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initErr != nil {
		return nil, m.initErr
	}
	id = strings.TrimSpace(id)
	task, ok := m.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	if _, running := m.running[id]; running {
		return nil, ErrTaskRunning
	}
	m.running[id] = struct{}{}
	return cloneTask(task), nil
}

func (m *Manager) executeReserved(parent context.Context, task *Task, trigger string, forceNotify bool) (RunRecord, error) {
	if trigger != TriggerScheduled {
		trigger = TriggerManual
	}
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-parent.Done():
		return m.finishRun(task, trigger, executionOutput{}, parent.Err(), forceNotify, m.now().UTC(), m.now().UTC())
	}

	startedAt := m.now().UTC()
	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds * time.Second
	}
	runCtx, cancel := context.WithTimeout(parent, timeout)
	output, runErr := m.executeTask(runCtx, task)
	cancel()
	finishedAt := m.now().UTC()
	return m.finishRun(task, trigger, output, runErr, forceNotify, startedAt, finishedAt)
}

func (m *Manager) executeTask(ctx context.Context, task *Task) (output executionOutput, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("job panic: %v", recovered)
		}
	}()
	payload := RunPayload{TaskID: task.ID, TaskName: task.Name, Input: cloneAnyMap(task.Input), Secrets: cloneStringMap(task.Secrets)}
	switch task.Kind {
	case TaskKindBuiltin:
		job, ok := lookupBuiltin(task.BuiltinID)
		if !ok {
			return output, fmt.Errorf("unknown built-in job: %s", task.BuiltinID)
		}
		result, runErr := job.handler(ctx, payload)
		output.Result = result
		return output, runErr
	case TaskKindJavaScript:
		return runJavaScript(ctx, task, payload, m.store.path)
	default:
		return output, fmt.Errorf("unsupported job kind: %s", task.Kind)
	}
}

func (m *Manager) finishRun(task *Task, trigger string, output executionOutput, runErr error, forceNotify bool, startedAt, finishedAt time.Time) (RunRecord, error) {
	result := normalizeResult(task, output.Result, runErr)
	record := RunRecord{
		ID: uuid.NewString(), TaskID: task.ID, TaskName: task.Name, Trigger: trigger,
		Status: result.Status, Title: result.Title, Message: result.Message,
		Data: cloneAnyMap(result.Data), Stdout: output.Stdout, Stderr: output.Stderr,
		StartedAt: startedAt, FinishedAt: finishedAt,
		DurationMS: finishedAt.Sub(startedAt).Milliseconds(),
	}
	if runErr != nil {
		record.Error = runErr.Error()
	}

	m.mu.Lock()
	current, exists := m.tasks[task.ID]
	if !exists {
		delete(m.running, task.ID)
		m.mu.Unlock()
		return record, ErrTaskNotFound
	}
	previousStatus := current.LastStatus
	for key, value := range result.secretUpdates {
		key = strings.TrimSpace(key)
		if key != "" && value != "" {
			current.Secrets[key] = value
		}
	}
	current.LastStatus = result.Status
	current.LastMessage = result.Message
	current.LastRunAt = cloneTimePtr(&finishedAt)
	current.UpdatedAt = finishedAt
	if current.Enabled {
		next := finishedAt.Add(time.Duration(current.IntervalSeconds) * time.Second)
		current.NextRunAt = &next
	} else {
		current.NextRunAt = nil
	}
	shouldNotify, notification := buildNotification(current, previousStatus, result, record, trigger, forceNotify, finishedAt)
	m.runs = append([]RunRecord{record}, m.runs...)
	if len(m.runs) > maxRunHistory {
		m.runs = m.runs[:maxRunHistory]
	}
	delete(m.running, task.ID)
	persistErr := m.persistLocked()
	notifier := m.notifier
	m.mu.Unlock()

	if persistErr != nil {
		return record, persistErr
	}
	if shouldNotify {
		notifyErr := errors.New("notification provider is not configured")
		if notifier != nil {
			notifyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			notifyErr = notifier(notifyCtx, notification)
			cancel()
		}
		record.Notified = notifyErr == nil
		if notifyErr != nil {
			record.NotifyError = notifyErr.Error()
		}
		m.recordNotificationResult(task.ID, record, notification, finishedAt, notifyErr)
	}
	return record, nil
}

func (m *Manager) recordNotificationResult(taskID string, record RunRecord, notification Notification, at time.Time, notifyErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.runs {
		if m.runs[i].ID == record.ID {
			m.runs[i].Notified = record.Notified
			m.runs[i].NotifyError = record.NotifyError
			break
		}
	}
	if notifyErr == nil {
		if task := m.tasks[taskID]; task != nil {
			task.LastNotificationAt = cloneTimePtr(&at)
			task.LastNotificationKey = notification.DedupKey
		}
	}
	if err := m.persistLocked(); err != nil {
		slog.Error("admin jobs: persist notification result failed", "task_id", taskID, "error", err)
	}
}

func normalizeResult(task *Task, result Result, runErr error) Result {
	if runErr != nil {
		result.Status = StatusError
		if result.Title == "" {
			result.Title = "任务执行失败"
		}
		if result.Message == "" {
			result.Message = runErr.Error()
		}
	}
	switch result.Status {
	case StatusOK, StatusWarning, StatusCritical, StatusError:
	default:
		result.Status = StatusOK
	}
	if strings.TrimSpace(result.Title) == "" {
		result.Title = task.Name
	}
	if strings.TrimSpace(result.Message) == "" {
		if result.Status == StatusOK {
			result.Message = "任务执行成功"
		} else {
			result.Message = "任务返回异常状态"
		}
	}
	if strings.TrimSpace(result.DedupKey) == "" {
		result.DedupKey = task.ID + ":" + result.Status
	}
	result.Data = cloneAnyMap(result.Data)
	return result
}

func buildNotification(task *Task, previousStatus string, result Result, record RunRecord, trigger string, force bool, now time.Time) (bool, Notification) {
	currentAbnormal := isAbnormalStatus(result.Status)
	previousAbnormal := isAbnormalStatus(previousStatus)
	shouldNotify := force
	if !force {
		if trigger == TriggerManual && !task.NotifyManual {
			return false, Notification{}
		}
		switch task.NotifyPolicy {
		case NotifyNever:
			return false, Notification{}
		case NotifyAlways:
			shouldNotify = true
		case NotifyFailure, NotifyFailureRecovery:
			if currentAbnormal {
				cooldown := time.Duration(task.CooldownSeconds) * time.Second
				if !previousAbnormal || task.LastNotificationAt == nil || result.DedupKey != task.LastNotificationKey || now.Sub(*task.LastNotificationAt) >= cooldown {
					shouldNotify = true
				}
			} else if task.NotifyPolicy == NotifyFailureRecovery && previousAbnormal {
				shouldNotify = true
			}
		}
	}
	if !shouldNotify {
		return false, Notification{}
	}

	severity := result.Status
	title := result.Title
	if !currentAbnormal && previousAbnormal {
		severity = "info"
		title = "任务恢复：" + task.Name
	} else if result.Status == StatusError || result.Status == StatusCritical {
		severity = StatusCritical
	}
	body := fmt.Sprintf("任务：%s\n状态：%s\n消息：%s\n耗时：%d ms\n时间：%s", task.Name, result.Status, result.Message, record.DurationMS, record.FinishedAt.Format("2006-01-02 15:04:05"))
	if record.Error != "" && record.Error != result.Message {
		body += "\n错误：" + record.Error
	}
	return true, Notification{
		TaskID: task.ID, TaskName: task.Name, Severity: severity,
		Title: title, Body: body, DedupKey: result.DedupKey,
	}
}

func isAbnormalStatus(status string) bool {
	return status == StatusWarning || status == StatusCritical || status == StatusError
}

func applyDraft(task *Task, draft TaskDraft, updating bool, now time.Time) error {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.Description = strings.TrimSpace(draft.Description)
	draft.Kind = strings.TrimSpace(draft.Kind)
	draft.BuiltinID = strings.TrimSpace(draft.BuiltinID)
	if draft.Kind == "" {
		draft.Kind = TaskKindBuiltin
	}
	if draft.TimeoutSeconds == 0 {
		draft.TimeoutSeconds = defaultTimeoutSeconds
	}
	if draft.IntervalSeconds == 0 {
		draft.IntervalSeconds = defaultIntervalSeconds
	}
	if draft.CooldownSeconds == 0 {
		draft.CooldownSeconds = defaultCooldownSeconds
	}
	if draft.NotifyPolicy == "" {
		draft.NotifyPolicy = NotifyFailureRecovery
	}

	if task.Input == nil {
		task.Input = map[string]any{}
	}
	if draft.Input != nil {
		task.Input = cloneAnyMap(draft.Input)
	}
	if task.Secrets == nil {
		task.Secrets = map[string]string{}
	}
	for _, key := range draft.ClearSecretKeys {
		delete(task.Secrets, strings.TrimSpace(key))
	}
	for key, value := range draft.Secrets {
		key = strings.TrimSpace(key)
		if key == "" || value == "" || value == "********" || value == "••••••••" {
			continue
		}
		task.Secrets[key] = value
	}

	if draft.Kind == TaskKindBuiltin {
		builtin, ok := lookupBuiltin(draft.BuiltinID)
		if !ok {
			return fmt.Errorf("unknown built-in job: %s", draft.BuiltinID)
		}
		if draft.Name == "" {
			draft.Name = builtin.definition.Name
		}
		applyBuiltinDefaults(task.Input, builtin.definition.Fields)
		if err := validateBuiltinFields(task, builtin.definition.Fields); err != nil {
			return err
		}
		if draft.BuiltinID == builtinSub2APIBalance &&
			strings.TrimSpace(task.Secrets["refresh_token"]) == "" &&
			strings.TrimSpace(task.Secrets["api_key"]) == "" {
			return errors.New("Refresh Token 与 API Key / Access Token 至少填写一个")
		}
		task.Script = ""
	} else if draft.Kind == TaskKindJavaScript {
		if strings.TrimSpace(draft.Script) == "" {
			return errors.New("JavaScript code is required")
		}
		if len(draft.Script) > maxTaskTextBytes {
			return errors.New("JavaScript code is too large")
		}
		task.Script = draft.Script
		draft.BuiltinID = ""
	} else {
		return fmt.Errorf("unsupported job kind: %s", draft.Kind)
	}

	if draft.Name == "" {
		return errors.New("job name is required")
	}
	if len(draft.Name) > 128 || len(draft.Description) > 1024 {
		return errors.New("job name or description is too long")
	}
	if draft.IntervalSeconds < minIntervalSeconds || draft.IntervalSeconds > maxIntervalSeconds {
		return fmt.Errorf("interval_seconds must be between %d and %d", minIntervalSeconds, maxIntervalSeconds)
	}
	if draft.TimeoutSeconds < 1 || draft.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between 1 and %d", maxTimeoutSeconds)
	}
	if draft.CooldownSeconds < 60 || draft.CooldownSeconds > maxIntervalSeconds {
		return fmt.Errorf("cooldown_seconds must be between 60 and %d", maxIntervalSeconds)
	}
	switch draft.NotifyPolicy {
	case NotifyNever, NotifyFailure, NotifyFailureRecovery, NotifyAlways:
	default:
		return fmt.Errorf("unsupported notify_policy: %s", draft.NotifyPolicy)
	}
	if inputJSON, err := json.Marshal(task.Input); err != nil || len(inputJSON) > maxTaskTextBytes {
		return errors.New("job input is invalid or too large")
	}
	for key, value := range task.Secrets {
		if len(key) > 128 || len(value) > 16*1024 {
			return errors.New("job secret key or value is too large")
		}
	}

	wasEnabled := task.Enabled
	oldInterval := task.IntervalSeconds
	task.Name = draft.Name
	task.Description = draft.Description
	task.Kind = draft.Kind
	task.BuiltinID = draft.BuiltinID
	task.Enabled = draft.Enabled
	task.IntervalSeconds = draft.IntervalSeconds
	task.TimeoutSeconds = draft.TimeoutSeconds
	task.NotifyPolicy = draft.NotifyPolicy
	task.NotifyManual = draft.NotifyManual
	task.CooldownSeconds = draft.CooldownSeconds
	task.UpdatedAt = now
	if !updating || (task.Enabled && (!wasEnabled || oldInterval != task.IntervalSeconds)) {
		next := now.Add(time.Duration(task.IntervalSeconds) * time.Second)
		task.NextRunAt = &next
	}
	if !task.Enabled {
		task.NextRunAt = nil
	}
	return nil
}

func applyBuiltinDefaults(input map[string]any, fields []FieldDefinition) {
	for _, field := range fields {
		if field.Secret || field.Default == nil {
			continue
		}
		if _, exists := input[field.Key]; !exists {
			input[field.Key] = field.Default
		}
	}
}

func validateBuiltinFields(task *Task, fields []FieldDefinition) error {
	for _, field := range fields {
		if !field.Required {
			continue
		}
		if field.Secret {
			if strings.TrimSpace(task.Secrets[field.Key]) == "" {
				return fmt.Errorf("%s is required", field.Label)
			}
			continue
		}
		value, exists := task.Input[field.Key]
		if !exists || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return fmt.Errorf("%s is required", field.Label)
		}
	}
	return nil
}

func cloneTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	cloned := *task
	cloned.Input = cloneAnyMap(task.Input)
	cloned.Secrets = cloneStringMap(task.Secrets)
	cloned.LastRunAt = cloneTimePtr(task.LastRunAt)
	cloned.NextRunAt = cloneTimePtr(task.NextRunAt)
	cloned.LastNotificationAt = cloneTimePtr(task.LastNotificationAt)
	return &cloned
}

func (m *Manager) persistLocked() error {
	tasks := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, cloneTask(task))
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	runs := append([]RunRecord(nil), m.runs...)
	return m.store.save(persistedState{Version: persistedStateVersion, Tasks: tasks, Runs: runs})
}
