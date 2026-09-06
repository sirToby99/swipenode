package entirecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/spf13/cobra"
)

const doctorSchemaVersion = "swipenode.doctor.v1"

type checkStatus string

const (
	checkPass checkStatus = "PASS"
	checkWarn checkStatus = "WARN"
	checkFail checkStatus = "FAIL"
)

type doctorCheck struct {
	Name    string      `json:"name"`
	Status  checkStatus `json:"status"`
	Message string      `json:"message"`
}

type doctorReport struct {
	SchemaVersion string        `json:"schema_version"`
	Overall       checkStatus   `json:"overall"`
	Checks        []doctorCheck `json:"checks"`
}

func newDoctorCommand(_ BuildInfo, deps Dependencies) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether SwipeNode is usable in this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runDoctor(cmd.Context(), deps)
			var writeErr error
			if asJSON {
				writeErr = writeIndentedJSON(cmd, report)
			} else {
				writeErr = writeDoctorHuman(cmd, report)
			}
			if writeErr != nil {
				return writeErr
			}
			if report.Overall == checkFail {
				return ErrDoctorFailed
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable machine-readable JSON")
	return command
}

func runDoctor(ctx context.Context, deps Dependencies) doctorReport {
	report := doctorReport{SchemaVersion: doctorSchemaVersion, Overall: checkPass, Checks: []doctorCheck{}}
	add := func(name string, status checkStatus, message string) {
		report.Checks = append(report.Checks, doctorCheck{Name: name, Status: status, Message: message})
		if status == checkFail {
			report.Overall = checkFail
		} else if status == checkWarn && report.Overall == checkPass {
			report.Overall = checkWarn
		}
	}

	gitPath, gitErr := deps.LookPath("git")
	if gitErr != nil {
		add("git_binary", checkFail, "git is required but was not found on PATH")
	} else {
		add("git_binary", checkPass, "git available at "+gitPath)
	}

	entire := entirectx.FromLookup(deps.Getenv)
	if entire.Available {
		message := "Entire external-command environment detected"
		if entire.CLIVersion != "" {
			message += " (CLI " + entire.CLIVersion + ")"
		}
		add("entire_environment", checkPass, message)
	} else {
		add("entire_environment", checkWarn, "standalone mode; Entire environment variables are absent")
	}

	cwd, cwdErr := deps.Getwd()
	if cwdErr != nil {
		add("repository_root", checkFail, "working directory unavailable: "+trimError(cwdErr))
	} else if root, err := entirectx.ResolveRepoRoot(entire, cwd); err != nil {
		add("repository_root", checkFail, trimError(err))
	} else if _, err := gitcontext.Inspect(ctx, root); err != nil {
		add("git_repository", checkFail, trimError(err))
	} else {
		add("repository_root", checkPass, root)
		add("git_repository", checkPass, "repository metadata is readable")
	}

	if entire.PluginDataDir == "" {
		if entire.Available {
			add("plugin_data", checkWarn, "ENTIRE_PLUGIN_DATA_DIR is absent; no local plugin storage is currently required")
		} else {
			add("plugin_data", checkPass, "standalone mode does not require a plugin data directory")
		}
	} else if err := probePluginData(entire); err != nil {
		add("plugin_data", checkFail, trimError(err))
	} else {
		add("plugin_data", checkPass, "plugin data directory is writable: "+entire.PluginDataDir)
	}

	if deps.MCPAvailable {
		add("mcp", checkPass, "MCP stdio functionality is built into this executable")
	} else {
		add("mcp", checkWarn, "MCP functionality is not available in this executable")
	}
	add("runtime_tools", checkPass, "no browser, cloud CLI, database, or LLM executable is required")
	add("network_binding", checkPass, "the Entire plugin opens no listening sockets")
	return report
}

func probePluginData(entire entirectx.Context) error {
	directory, err := entirectx.EnsurePluginDataDir(entire)
	if err != nil {
		return err
	}
	probe, err := os.CreateTemp(directory, ".swipenode-doctor.*")
	if err != nil {
		return fmt.Errorf("plugin data directory is not writable: %w", err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return fmt.Errorf("close plugin data probe: %w", closeErr)
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("remove plugin data probe: %w", removeErr)
	}
	if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return fmt.Errorf("plugin data path is not a directory")
	}
	if filepath.Clean(directory) != directory {
		return fmt.Errorf("plugin data path is not normalized")
	}
	return nil
}

func writeDoctorHuman(cmd *cobra.Command, report doctorReport) error {
	var lines []string
	for _, check := range report.Checks {
		lines = append(lines, fmt.Sprintf("%-4s %-20s %s", check.Status, check.Name, check.Message))
	}
	lines = append(lines, "Overall: "+string(report.Overall))
	_, err := fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines, "\n"))
	return err
}
