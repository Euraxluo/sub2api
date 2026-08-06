package admin_jobs

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagerNotifiesOnFailureAndRecovery(t *testing.T) {
	builtinID := "test-transition-" + uuid.NewString()
	var runMu sync.Mutex
	runCount := 0
	RegisterBuiltin(BuiltinDefinition{ID: builtinID, Name: "状态切换测试"}, func(context.Context, RunPayload) (Result, error) {
		runMu.Lock()
		defer runMu.Unlock()
		runCount++
		if runCount == 1 {
			return Result{Status: StatusWarning, Title: "余额不足", Message: "balance=1", DedupKey: "balance"}, nil
		}
		return Result{Status: StatusOK, Title: "余额恢复", Message: "balance=20", DedupKey: "balance"}, nil
	})

	manager, err := NewManager(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	var notifications []Notification
	manager.SetNotifier(func(_ context.Context, notification Notification) error {
		notifications = append(notifications, notification)
		return nil
	})
	task, err := manager.CreateTask(TaskDraft{
		Name: "上游余额", Kind: TaskKindBuiltin, BuiltinID: builtinID,
		Enabled: false, IntervalSeconds: 60, TimeoutSeconds: 5,
		NotifyPolicy: NotifyFailureRecovery, NotifyManual: true, CooldownSeconds: 60,
	})
	require.NoError(t, err)
	require.Empty(t, task.SecretKeys)

	first, err := manager.Run(context.Background(), task.ID, TriggerManual, false)
	require.NoError(t, err)
	require.Equal(t, StatusWarning, first.Status)
	require.True(t, first.Notified)

	second, err := manager.Run(context.Background(), task.ID, TriggerManual, false)
	require.NoError(t, err)
	require.Equal(t, StatusOK, second.Status)
	require.True(t, second.Notified)

	require.Len(t, notifications, 2)
	require.Equal(t, StatusWarning, notifications[0].Severity)
	require.Equal(t, "info", notifications[1].Severity)
	runs, err := manager.ListRuns(task.ID, 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, second.ID, runs[0].ID)
}

func TestTaskViewReturnsSecretNamesWithoutValues(t *testing.T) {
	builtinID := "test-secret-" + uuid.NewString()
	RegisterBuiltin(BuiltinDefinition{
		ID: builtinID, Name: "密钥测试",
		Fields: []FieldDefinition{{Key: "token", Label: "Token", Type: "password", Secret: true, Required: true}},
	}, func(context.Context, RunPayload) (Result, error) {
		return Result{Status: StatusOK}, nil
	})
	manager, err := NewManager(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	view, err := manager.CreateTask(TaskDraft{
		Name: "密钥测试", Kind: TaskKindBuiltin, BuiltinID: builtinID,
		Secrets:         map[string]string{"token": "super-secret"},
		IntervalSeconds: 60, TimeoutSeconds: 5,
		NotifyPolicy: NotifyNever, CooldownSeconds: 60,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"token"}, view.SecretKeys)
}

func TestSub2APIUpstreamBalanceAcceptsPasswordLoginCredentials(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	view, err := manager.CreateTask(TaskDraft{
		Name: "上游余额", Kind: TaskKindBuiltin, BuiltinID: builtinSub2APIBalance,
		Input:           map[string]any{"base_url": "https://upstream.example", "login_email": "monitor@example.com"},
		Secrets:         map[string]string{"login_password": "password-test"},
		IntervalSeconds: 60, TimeoutSeconds: 5,
		NotifyPolicy: NotifyNever, CooldownSeconds: 60,
	})
	require.NoError(t, err)
	require.Contains(t, view.SecretKeys, "login_password")

	_, err = manager.CreateTask(TaskDraft{
		Name: "上游余额", Kind: TaskKindBuiltin, BuiltinID: builtinSub2APIBalance,
		Input:           map[string]any{"base_url": "https://upstream.example", "login_email": "monitor@example.com"},
		IntervalSeconds: 60, TimeoutSeconds: 5,
		NotifyPolicy: NotifyNever, CooldownSeconds: 60,
	})
	require.EqualError(t, err, "登录账号与登录密码必须同时填写")
}

func TestManagerPersistsBuiltinSecretRotation(t *testing.T) {
	builtinID := "test-secret-rotation-" + uuid.NewString()
	RegisterBuiltin(BuiltinDefinition{
		ID: builtinID, Name: "密钥轮换测试",
		Fields: []FieldDefinition{{Key: "token", Label: "Token", Type: "password", Secret: true, Required: true}},
	}, func(context.Context, RunPayload) (Result, error) {
		return Result{Status: StatusOK, secretUpdates: map[string]string{"token": "rotated-secret"}}, nil
	})

	storePath := filepath.Join(t.TempDir(), "state.json")
	manager, err := NewManager(storePath)
	require.NoError(t, err)
	view, err := manager.CreateTask(TaskDraft{
		Name: "密钥轮换测试", Kind: TaskKindBuiltin, BuiltinID: builtinID,
		Secrets:         map[string]string{"token": "initial-secret"},
		IntervalSeconds: 60, TimeoutSeconds: 5,
		NotifyPolicy: NotifyNever, CooldownSeconds: 60,
	})
	require.NoError(t, err)

	_, err = manager.Run(context.Background(), view.ID, TriggerManual, false)
	require.NoError(t, err)
	require.Equal(t, "rotated-secret", manager.tasks[view.ID].Secrets["token"])

	reloaded, err := NewManager(storePath)
	require.NoError(t, err)
	require.Equal(t, "rotated-secret", reloaded.tasks[view.ID].Secrets["token"])
	storedView, err := reloaded.GetTask(view.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"token"}, storedView.SecretKeys)
}
