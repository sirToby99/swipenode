package cmd

import (
	"github.com/sirToby99/swipenode/internal/verifycmd"
	"github.com/spf13/cobra"
)

func newStandaloneVerifyCommand(deps verifycmd.Dependencies) *cobra.Command {
	return verifycmd.NewCommand(deps)
}

func init() {
	rootCmd.AddCommand(newStandaloneVerifyCommand(verifycmd.DefaultDependencies()))
}
