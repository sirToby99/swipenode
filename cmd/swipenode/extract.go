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
	Short: "Extract Next.js hydration data from a URL",
	Run: func(cmd *cobra.Command, args []string) {
		if extractURL == "" {
			fmt.Fprintln(os.Stderr, "error: --url is required")
			os.Exit(1)
		}

		data, err := extractor.ExtractNextData(extractURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(data)
	},
}

func init() {
	extractCmd.Flags().StringVar(&extractURL, "url", "", "target URL to extract __NEXT_DATA__ from")
	rootCmd.AddCommand(extractCmd)
}
