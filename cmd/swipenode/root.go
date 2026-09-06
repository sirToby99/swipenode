package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "swipenode",
	Short:        "Source-backed engineering verification for customer-hosted workflows",
	Long:         "SwipeNode resolves reviewed Knowledge Packs, records Evidence, and persists conservative Verification, Audit, and Provenance state in the customer's local repository context.",
	Version:      buildVersion(),
	SilenceUsage: true,
}

// Execute runs the customer CLI. Managed service and Internal Admin commands
// are deliberately absent from the public customer distribution.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
