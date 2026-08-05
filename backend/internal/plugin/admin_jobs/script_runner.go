package admin_jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const scriptOutputLimit = 64 * 1024

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = b.buffer.Write(data[:remaining])
			b.truncated = true
		} else {
			_, _ = b.buffer.Write(data)
		}
	} else if len(data) > 0 {
		b.truncated = true
	}
	return written, nil
}

func (b *limitedBuffer) String() string {
	value := b.buffer.String()
	if b.truncated {
		value += "\n...[output truncated]"
	}
	return value
}

func runJavaScript(ctx context.Context, task *Task, payload RunPayload, storePath string) (executionOutput, error) {
	var output executionOutput
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return output, fmt.Errorf("Node.js is not installed: %w", err)
	}

	scriptDir := filepath.Join(filepath.Dir(storePath), "scripts")
	if err := os.MkdirAll(scriptDir, 0o700); err != nil {
		return output, fmt.Errorf("create script directory: %w", err)
	}
	scriptFile, err := os.CreateTemp(scriptDir, ".admin-job-*.mjs")
	if err != nil {
		return output, fmt.Errorf("create temporary script: %w", err)
	}
	scriptPath := scriptFile.Name()
	defer func() { _ = os.Remove(scriptPath) }()
	if err := scriptFile.Chmod(0o600); err != nil {
		_ = scriptFile.Close()
		return output, fmt.Errorf("secure temporary script: %w", err)
	}
	wrapper := buildJavaScriptWrapper(task.Script)
	if _, err := scriptFile.WriteString(wrapper); err != nil {
		_ = scriptFile.Close()
		return output, fmt.Errorf("write temporary script: %w", err)
	}
	if err := scriptFile.Close(); err != nil {
		return output, fmt.Errorf("close temporary script: %w", err)
	}

	input, err := json.Marshal(map[string]any{
		"input":   payload.Input,
		"secrets": payload.Secrets,
		"task": map[string]any{
			"id": payload.TaskID, "name": payload.TaskName,
		},
	})
	if err != nil {
		return output, fmt.Errorf("encode script input: %w", err)
	}

	stdout := &limitedBuffer{limit: scriptOutputLimit}
	stderr := &limitedBuffer{limit: scriptOutputLimit}
	cmd := exec.CommandContext(ctx, nodePath, scriptPath)
	cmd.Dir = scriptDir
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = []string{
		"HOME=" + scriptDir,
		"TMPDIR=" + scriptDir,
		"LANG=C.UTF-8",
		"NODE_NO_WARNINGS=1",
		"ADMIN_JOB_ID=" + task.ID,
		"ADMIN_JOB_NAME=" + task.Name,
	}
	runErr := cmd.Run()
	output.Stdout = stdout.String()
	output.Stderr = stderr.String()
	if ctx.Err() != nil {
		return output, fmt.Errorf("JavaScript timed out or was cancelled: %w", ctx.Err())
	}
	if runErr != nil {
		message := strings.TrimSpace(output.Stderr)
		if message == "" {
			message = strings.TrimSpace(output.Stdout)
		}
		if message != "" {
			return output, fmt.Errorf("JavaScript failed: %w: %s", runErr, tailText(message, 1000))
		}
		return output, fmt.Errorf("JavaScript failed: %w", runErr)
	}
	result, err := parseJavaScriptResult(output.Stdout)
	if err != nil {
		return output, err
	}
	output.Result = result
	return output, nil
}

func buildJavaScriptWrapper(script string) string {
	return `const __chunks = [];
for await (const __chunk of process.stdin) __chunks.push(__chunk);
const __rawInput = Buffer.concat(__chunks).toString("utf8");
const __payload = __rawInput ? JSON.parse(__rawInput) : {};
const input = __payload.input || {};
const secrets = __payload.secrets || {};
const task = __payload.task || {};
const result = await (async () => {
` + script + `
})();
if (result !== undefined) process.stdout.write(JSON.stringify(result) + "\n");
`
}

func parseJavaScriptResult(stdout string) (Result, error) {
	lines := strings.Split(stdout, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var result Result
		if err := json.Unmarshal([]byte(line), &result); err == nil {
			return result, nil
		}
		var message string
		if err := json.Unmarshal([]byte(line), &message); err == nil {
			return Result{Status: StatusOK, Message: message}, nil
		}
	}
	return Result{}, fmt.Errorf("JavaScript must return a result object or print JSON as its final line; stdout=%s", tailText(strings.TrimSpace(stdout), 1000))
}

func tailText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
