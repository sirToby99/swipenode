// Package agentinstructions safely manages SwipeNode's generated AGENTS.md
// section without owning any surrounding user content.
package agentinstructions

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	BeginMarker = "<!-- BEGIN SWIPENODE INSTRUCTIONS -->"
	EndMarker   = "<!-- END SWIPENODE INSTRUCTIONS -->"
)

const generatedSection = `<!-- BEGIN SWIPENODE INSTRUCTIONS -->
## SwipeNode external fact verification

- Use SwipeNode when an implementation depends on external technical facts.
- Prefer authoritative primary sources for hardware, API, protocol, standard, compatibility, and version assumptions.
- Verify externally sourced assumptions before coding when they can affect correctness or safety.
- Run ` + "`entire swipenode verify`" + ` before finalizing changes that depend on external facts.
- Use repository-local search for purely internal questions; SwipeNode is not a replacement for reading this repository.
<!-- END SWIPENODE INSTRUCTIONS -->`

type Result struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Changed bool   `json:"changed"`
	Content string `json:"content,omitempty"`
}

// Update computes and optionally writes the generated section.
func Update(repoRoot string, dryRun bool) (Result, error) {
	path := filepath.Join(repoRoot, "AGENTS.md")
	previous, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("read AGENTS.md: %w", err)
	}
	next, action, err := Merge(string(previous))
	if err != nil {
		return Result{}, err
	}
	changed := !bytes.Equal(previous, []byte(next))
	result := Result{Path: path, Action: action, Changed: changed}
	if dryRun {
		result.Content = next
		return result, nil
	}
	if !changed {
		return result, nil
	}
	mode := os.FileMode(0644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := writeAtomically(path, []byte(next), mode); err != nil {
		return Result{}, err
	}
	return result, nil
}

// Merge is exported for deterministic unit testing.
func Merge(previous string) (string, string, error) {
	if strings.Count(previous, BeginMarker) != strings.Count(previous, EndMarker) {
		return "", "", fmt.Errorf("AGENTS.md contains an incomplete or malformed SwipeNode generated section")
	}
	if strings.Contains(previous, BeginMarker) {
		var merged strings.Builder
		cursor := 0
		inserted := false
		for {
			beginOffset := strings.Index(previous[cursor:], BeginMarker)
			if beginOffset < 0 {
				merged.WriteString(previous[cursor:])
				break
			}
			begin := cursor + beginOffset
			endOffset := strings.Index(previous[begin+len(BeginMarker):], EndMarker)
			if endOffset < 0 {
				return "", "", fmt.Errorf("AGENTS.md contains an incomplete or malformed SwipeNode generated section")
			}
			end := begin + len(BeginMarker) + endOffset
			if nested := strings.Index(previous[begin+len(BeginMarker):end], BeginMarker); nested >= 0 {
				return "", "", fmt.Errorf("AGENTS.md contains nested SwipeNode generated sections")
			}
			merged.WriteString(previous[cursor:begin])
			if !inserted {
				merged.WriteString(generatedSection)
				inserted = true
			}
			cursor = end + len(EndMarker)
		}
		return normalizeFinalNewline(merged.String()), "updated", nil
	}
	prefix := strings.TrimRight(previous, "\r\n")
	if prefix == "" {
		return generatedSection + "\n", "created", nil
	}
	return prefix + "\n\n" + generatedSection + "\n", "appended", nil
}

func writeAtomically(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create AGENTS.md directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".AGENTS.md.swipenode.*")
	if err != nil {
		return fmt.Errorf("create temporary AGENTS.md: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set AGENTS.md permissions: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary AGENTS.md: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary AGENTS.md: %w", err)
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return fmt.Errorf("replace AGENTS.md atomically: %w", err)
	}
	return nil
}

func normalizeFinalNewline(value string) string {
	return strings.TrimRight(value, "\r\n") + "\n"
}
