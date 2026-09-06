// Package entireprovenance enriches SwipeNode-owned records through Entire's
// documented, read-only external-command and session JSON contracts.
package entireprovenance

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/sirToby99/swipenode/internal/provenance"
)

const attachmentLimitation = "Entire external commands do not expose a checkpoint metadata attachment API; this linkage is stored in the SwipeNode provenance record."

type RunFunc func(context.Context, string, string, ...string) ([]byte, error)

type Dependencies struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Run      RunFunc
}

type Adapter struct{ deps Dependencies }

func New(deps Dependencies) Adapter {
	if deps.Getenv == nil {
		deps.Getenv = os.Getenv
	}
	if deps.LookPath == nil {
		deps.LookPath = exec.LookPath
	}
	if deps.Run == nil {
		deps.Run = runCommand
	}
	return Adapter{deps: deps}
}

func Default() Adapter { return New(Dependencies{}) }

type sessionInfo struct {
	SessionID      string   `json:"session_id"`
	Agent          string   `json:"agent"`
	Model          string   `json:"model,omitempty"`
	Status         string   `json:"status"`
	WorktreePath   string   `json:"worktree_path,omitempty"`
	LastCheckpoint string   `json:"last_checkpoint_id,omitempty"`
	FilesTouched   []string `json:"files_touched,omitempty"`
}

func (adapter Adapter) Enrich(ctx context.Context, repositoryRoot string) provenance.Enrichment {
	entire := &provenance.EntireContext{ContextStatus: "unavailable", Limitation: attachmentLimitation}
	result := provenance.Enrichment{Adapter: "entire", ExternalContext: &provenance.ExternalContext{Entire: entire}}
	contract := entirectx.FromLookup(adapter.deps.Getenv)
	entire.CLIVersion = safeIdentifier(contract.CLIVersion)
	if !contract.Available || contract.RepoRoot == "" || entire.CLIVersion == "" {
		return result
	}
	root, err := filepath.Abs(filepath.Clean(repositoryRoot))
	if err != nil || filepath.Clean(contract.RepoRoot) != root {
		entire.ContextStatus = "invalid"
		return result
	}
	binary, err := adapter.deps.LookPath("entire")
	if err != nil {
		return result
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := adapter.deps.Run(queryCtx, root, binary, "session", "current", "--json")
	if err != nil || len(output) > 1<<20 {
		return result
	}
	var session sessionInfo
	if err := json.Unmarshal(output, &session); err != nil {
		entire.ContextStatus = "invalid"
		return result
	}
	if session.SessionID == "" || session.WorktreePath == "" {
		entire.ContextStatus = "invalid"
		return result
	}
	worktree, err := filepath.Abs(filepath.Clean(session.WorktreePath))
	if err != nil || worktree != root {
		entire.ContextStatus = "invalid"
		return result
	}
	entire.SessionID = safeIdentifier(session.SessionID)
	entire.CheckpointID = safeIdentifier(session.LastCheckpoint)
	entire.Agent = safeLabel(session.Agent)
	entire.Model = safeLabel(session.Model)
	entire.SessionStatus = safeIdentifier(session.Status)
	if entire.SessionID == "" {
		entire.ContextStatus = "invalid"
		return result
	}
	entire.ContextStatus = "available"
	for _, path := range session.FilesTouched {
		path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
		if filepath.IsLocal(path) && path != "." {
			entire.SessionArtifacts = append(entire.SessionArtifacts, provenance.Artifact{Path: path, Kind: "file"})
		}
	}
	return result
}

func safeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n\t") {
		return ""
	}
	return value
}

func safeLabel(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 256 {
		return ""
	}
	return value
}

func runCommand(ctx context.Context, dir, binary string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	stdout := &limitedBuffer{limit: 1 << 20}
	command.Stdout = stdout
	command.Stderr = &limitedBuffer{limit: 16 << 10}
	if err := command.Run(); err != nil {
		return nil, err
	}
	if stdout.overflow {
		return nil, context.DeadlineExceeded
	}
	return append([]byte(nil), stdout.buffer.Bytes()...), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.buffer.Write(value)
	}
	if original > remaining {
		buffer.overflow = true
	}
	return original, nil
}
