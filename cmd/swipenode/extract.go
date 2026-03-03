// cmd/swipenode/extract.go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/swipenode-local/swipenode/pkg/extractor"
)

var extractURL string

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract structured data from a URL (Next.js, Nuxt.js, or clean text)",
	Run: func(cmd *cobra.Command, args []string) {
		if extractURL == "" {
			fmt.Fprintln(os.Stderr, "error: --url is required")
			os.Exit(1)
		}

		data, err := extractor.ExtractData(extractURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(data)
	},
}

func init() {
	extractCmd.Flags().StringVar(&extractURL, "url", "", "target URL to extract data from")
	rootCmd.AddCommand(extractCmd)
}
