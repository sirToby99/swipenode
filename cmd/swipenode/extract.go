// cmd/swipenode/extract.go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sirToby99/swipenode/pkg/extractor"
)

var extractURL string
var impersonate string

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract structured data from a URL (Next.js, Nuxt.js, or clean text)",
	Run: func(cmd *cobra.Command, args []string) {
		data, err := extractor.ExtractData(extractURL, impersonate)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		fmt.Println(data)
	},
}

func init() {
	extractCmd.Flags().StringVar(&extractURL, "url", "", "target URL to extract data from")
	extractCmd.Flags().StringVar(&impersonate, "impersonate", "chrome", "Browser-Fingerprint (chrome, safari, firefox)")
	rootCmd.AddCommand(extractCmd)
}
