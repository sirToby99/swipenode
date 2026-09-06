package entireprovenance

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdapterMissingEntireContextIsNonfatal(t *testing.T) {
	adapter := New(Dependencies{
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", errors.New("must not execute") },
	})
	result := adapter.Enrich(context.Background(), t.TempDir())
	if result.Adapter != "entire" || result.ExternalContext == nil || result.ExternalContext.Entire.ContextStatus != "unavailable" {
		t.Fatalf("unexpected missing context: %#v", result)
	}
}

func TestAdapterEnrichesOnlyDocumentedBoundedSessionFields(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.Abs(root)
	values := map[string]string{"ENTIRE_REPO_ROOT": root, "ENTIRE_CLI_VERSION": "0.10.3", "ENTIRE_PLUGIN_DATA_DIR": filepath.Join(root, "ignored")}
	adapter := New(Dependencies{
		Getenv:   func(key string) string { return values[key] },
		LookPath: func(string) (string, error) { return "/usr/bin/entire", nil },
		Run: func(_ context.Context, dir, binary string, args ...string) ([]byte, error) {
			if dir != root || binary != "/usr/bin/entire" || strings.Join(args, " ") != "session current --json" {
				t.Fatalf("unexpected command: %q %q %q", dir, binary, args)
			}
			return []byte(`{"session_id":"session-1","agent":"codex","model":"gpt-test","status":"active","worktree_path":"` + root + `","last_checkpoint_id":"checkpoint-1","files_touched":["robot.urdf","config/bom.yaml","../outside"],"last_prompt":"api_key=must-not-be-captured","tokens":{"input":99}}`), nil
		},
	})
	result := adapter.Enrich(context.Background(), root)
	ctx := result.ExternalContext.Entire
	if ctx.ContextStatus != "available" || ctx.SessionID != "session-1" || ctx.CheckpointID != "checkpoint-1" || len(ctx.SessionArtifacts) != 2 {
		t.Fatalf("unexpected enrichment: %#v", result)
	}
	if strings.Contains(strings.ToLower(ctx.Limitation), "api_key") {
		t.Fatalf("prompt leaked into context: %#v", ctx)
	}
}

func TestAdapterRejectsMismatchedOrInvalidEntireContext(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	values := map[string]string{"ENTIRE_REPO_ROOT": root, "ENTIRE_CLI_VERSION": "0.10.3"}
	adapter := New(Dependencies{
		Getenv:   func(key string) string { return values[key] },
		LookPath: func(string) (string, error) { return "entire", nil },
		Run: func(context.Context, string, string, ...string) ([]byte, error) {
			return []byte(`{"session_id":"wrong","worktree_path":"` + other + `"}`), nil
		},
	})
	if status := adapter.Enrich(context.Background(), root).ExternalContext.Entire.ContextStatus; status != "invalid" {
		t.Fatalf("mismatched session worktree accepted: %s", status)
	}
	values["ENTIRE_REPO_ROOT"] = other
	if status := adapter.Enrich(context.Background(), root).ExternalContext.Entire.ContextStatus; status != "invalid" {
		t.Fatalf("mismatched environment root accepted: %s", status)
	}
}

func TestAdapterTreatsMissingSessionAsUnavailableAndMalformedJSONAsInvalid(t *testing.T) {
	root := t.TempDir()
	values := map[string]string{"ENTIRE_REPO_ROOT": root, "ENTIRE_CLI_VERSION": "0.10.3"}
	runOutput := []byte(nil)
	runErr := errors.New("no current session")
	adapter := New(Dependencies{
		Getenv:   func(key string) string { return values[key] },
		LookPath: func(string) (string, error) { return "entire", nil },
		Run:      func(context.Context, string, string, ...string) ([]byte, error) { return runOutput, runErr },
	})
	if status := adapter.Enrich(context.Background(), root).ExternalContext.Entire.ContextStatus; status != "unavailable" {
		t.Fatalf("missing session should be nonfatal, got %s", status)
	}
	runOutput, runErr = []byte(`{"session_id":`), nil
	if status := adapter.Enrich(context.Background(), root).ExternalContext.Entire.ContextStatus; status != "invalid" {
		t.Fatalf("malformed session JSON should be invalid, got %s", status)
	}
}
