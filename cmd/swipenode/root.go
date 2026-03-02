// cmd/swipenode/root.go
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "swipenode",
	Short: "SwipeNode - zero-render extraction for AI agents",
	Long:  "A lightning-fast CLI that extracts structured data from raw HTML without headless browsers.",
}

// Execute runs the root command. Called from main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
