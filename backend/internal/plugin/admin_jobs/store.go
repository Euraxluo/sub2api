package admin_jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const persistedStateVersion = 1

type persistedState struct {
	Version int         `json:"version"`
	Tasks   []*Task     `json:"tasks"`
	Runs    []RunRecord `json:"runs"`
}

type fileStore struct {
	path string
}

func defaultStorePath() string {
	if configured := strings.TrimSpace(os.Getenv("SUB2API_ADMIN_JOBS_STORE")); configured != "" {
		return configured
	}
	if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
		return "/app/data/admin-jobs/state.json"
	}
	return "./data/admin-jobs/state.json"
}

func (s fileStore) load() (persistedState, error) {
	state := persistedState{Version: persistedStateVersion, Tasks: []*Task{}, Runs: []RunRecord{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("read admin jobs state: %w", err)
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, fmt.Errorf("parse admin jobs state: %w", err)
	}
	if state.Version != persistedStateVersion {
		return persistedState{}, fmt.Errorf("unsupported admin jobs state version: %d", state.Version)
	}
	if state.Tasks == nil {
		state.Tasks = []*Task{}
	}
	if state.Runs == nil {
		state.Runs = []RunRecord{}
	}
	return state, nil
}

func (s fileStore) save(state persistedState) error {
	state.Version = persistedStateVersion
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode admin jobs state: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create admin jobs directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".admin-jobs-*.tmp")
	if err != nil {
		return fmt.Errorf("create admin jobs state temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("secure admin jobs state temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write admin jobs state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync admin jobs state: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close admin jobs state: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace admin jobs state: %w", err)
	}
	return nil
}
