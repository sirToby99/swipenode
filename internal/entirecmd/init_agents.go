package entirecmd

import (
	"fmt"

	"github.com/sirToby99/swipenode/internal/agentinstructions"
	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/spf13/cobra"
)

func newInitAgentsCommand(deps Dependencies) *cobra.Command {
	var dryRun bool
	var asJSON bool
	command := &cobra.Command{
		Use:   "init-agents",
		Short: "Safely add SwipeNode guidance to AGENTS.md",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := deps.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			root, err := entirectx.ResolveRepoRoot(entirectx.FromLookup(deps.Getenv), cwd)
			if err != nil {
				return err
			}
			result, err := agentinstructions.Update(root, dryRun)
			if err != nil {
				return err
			}
			if asJSON {
				return writeIndentedJSON(cmd, result)
			}
			if dryRun {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would %s %s\n\n%s", futureAction(result.Action), result.Path, result.Content)
				return err
			}
			if result.Changed {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s SwipeNode instructions in %s\n", titleAction(result.Action), result.Path)
			} else {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "SwipeNode instructions already up to date in %s\n", result.Path)
			}
			return err
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the result without writing AGENTS.md")
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable machine-readable JSON")
	return command
}

func titleAction(value string) string {
	switch value {
	case "created":
		return "Created"
	case "appended":
		return "Added"
	default:
		return "Updated"
	}
}

func futureAction(value string) string {
	switch value {
	case "created":
		return "create"
	case "appended":
		return "append to"
	default:
		return "update"
	}
}
