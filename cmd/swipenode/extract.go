// cmd/swipenode/extract.go
package cmd

import (
	"fmt"

	"github.com/sirToby99/swipenode/pkg/extractor"
	"github.com/spf13/cobra"
)

var extractURL string
var extractFile string
var impersonate string

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract structured data from a URL (Next.js, Nuxt.js, or clean text)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if extractURL != "" && extractFile != "" {
			return fmt.Errorf("provide exactly one of --url or --file")
		}
		var data string
		var err error

		if extractFile != "" {
			data, err = extractor.ExtractDataFromFile(extractFile)
		} else if extractURL != "" {
			data, err = extractor.ExtractData(extractURL, impersonate)
		} else {
			return fmt.Errorf("provide exactly one of --url or --file")
		}

		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		fmt.Println(data)
		return nil
	},
}

func init() {
	extractCmd.Flags().StringVar(&extractURL, "url", "", "target URL to extract data from")
	extractCmd.Flags().StringVar(&extractFile, "file", "", "parse local HTML file")
	extractCmd.Flags().StringVar(&impersonate, "impersonate", "chrome", "compatibility header profile (chrome, safari, firefox; no TLS impersonation)")
	rootCmd.AddCommand(extractCmd)
}
