package cmd

import "github.com/sirToby99/swipenode/internal/trustcmd"

func init() { rootCmd.AddCommand(trustcmd.New()) }
