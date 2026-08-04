package claw163

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAuthURL(t *testing.T) {
	got, err := normalizeAuthURL("t1/example")
	if err != nil || got != "https://u.163.com/t1/example" {
		t.Fatalf("normalizeAuthURL() = %q, %v", got, err)
	}
	if _, err := normalizeAuthURL("https://example.com/t1/example"); err == nil {
		t.Fatal("expected untrusted host to fail")
	}
}

func TestParseAuthPayload(t *testing.T) {
	apiKey, accounts, err := parseAuthPayload("__apikey__:workspace:key\nalice:default:\nbob:imap:app-password")
	if err != nil {
		t.Fatal(err)
	}
	if apiKey != "key" || len(accounts) != 2 {
		t.Fatalf("unexpected payload: %q %#v", apiKey, accounts)
	}
	if accounts[0].Transport != "ws" || accounts[1].Transport != "imap" {
		t.Fatalf("unexpected transports: %#v", accounts)
	}
}

func TestManagerInitializeAndSendUsesCLI(t *testing.T) {
	profile := t.TempDir()

	var calls [][]string
	manager := NewManager(profile)
	manager.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("__apikey__::key\nalice:default:\n")),
			Request:    request,
		}, nil
	})}
	manager.now = func() time.Time { return time.Unix(10, 0) }
	manager.runner = func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) == 1 && args[0] == "--version" {
			return []byte("0.2.4"), nil
		}
		return []byte(`{"success":true}`), nil
	}

	status, err := manager.Initialize(context.Background(), "t1/example", []string{"notify@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Initialized || !status.Ready || status.Sender != "alice@claw.163.com" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if err := manager.Send(context.Background(), "subject", "body", nil); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "auth apikey set key") || !strings.Contains(joined, "auth login --user alice@claw.163.com") || !strings.Contains(joined, "compose send") {
		t.Fatalf("expected CLI calls, got:\n%s", joined)
	}
	state, err := os.ReadFile(filepath.Join(profile, "notification-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "key") || strings.Contains(string(state), "t1/example") {
		t.Fatalf("secret leaked to state: %s", state)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
