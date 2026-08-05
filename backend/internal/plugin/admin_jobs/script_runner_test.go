package admin_jobs

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunJavaScriptReturnsStructuredResult(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is not installed")
	}
	task := &Task{
		ID: "script-task", Name: "脚本测试",
		Script: `return {
  status: input.balance < input.threshold ? 'warning' : 'ok',
  title: task.name,
  message: secrets.token ? 'token-present' : 'token-missing',
  data: { balance: input.balance }
}`,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := runJavaScript(ctx, task, RunPayload{
		TaskID: task.ID, TaskName: task.Name,
		Input:   map[string]any{"balance": 2.0, "threshold": 10.0},
		Secrets: map[string]string{"token": "secret"},
	}, filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	require.Equal(t, StatusWarning, output.Result.Status)
	require.Equal(t, "token-present", output.Result.Message)
	require.Equal(t, float64(2), output.Result.Data["balance"])
}
