package cmd

import "github.com/sirToby99/swipenode/internal/provenancecmd"

func init() {
	rootCmd.AddCommand(provenancecmd.NewCommand(provenancecmd.DefaultDependencies()))
}
