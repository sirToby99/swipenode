// cmd/swipenode/batch.go
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/swipenode-local/swipenode/pkg/extractor"
)

// BatchResult holds the outcome of a single URL extraction.
type BatchResult struct {
	URL   string          `json:"url"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

var (
	batchFile        string
	batchImpersonate string
	batchConcurrency int
	batchOut         string
)

var batchCmd = &cobra.Command{
	Use:   "batch",
	Short: "Extract structured data from a list of URLs concurrently",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Read the input file.
		f, err := os.Open(batchFile)
		if err != nil {
			return fmt.Errorf("opening file: %w", err)
		}
		defer f.Close()

		var urls []string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			urls = append(urls, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("reading file: %w", err)
		}

		if len(urls) == 0 {
			return fmt.Errorf("no URLs found in %s", batchFile)
		}

		// Set up channels and worker pool.
		urlsChan := make(chan string, len(urls))
		resultsChan := make(chan BatchResult, len(urls))

		var wg sync.WaitGroup
		for i := 0; i < batchConcurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for u := range urlsChan {
					raw, err := extractor.ExtractData(u, batchImpersonate)
					var result BatchResult
					result.URL = u
					if err != nil {
						result.Error = err.Error()
					} else {
						// If the output is valid JSON, embed it directly.
						// Otherwise wrap the plain text as a JSON string.
						trimmed := strings.TrimSpace(raw)
						if json.Valid([]byte(trimmed)) {
							result.Data = json.RawMessage(trimmed)
						} else {
							quoted, _ := json.Marshal(trimmed)
							result.Data = json.RawMessage(quoted)
						}
					}
					resultsChan <- result
				}
			}()
		}

		// Feed URLs to workers.
		for _, u := range urls {
			urlsChan <- u
		}
		close(urlsChan)

		// Close results channel once all workers finish.
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Collect results.
		var results []BatchResult
		for r := range resultsChan {
			results = append(results, r)
		}

		// Marshal and write output.
		out, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling results: %w", err)
		}

		if err := os.WriteFile(batchOut, out, 0644); err != nil {
			return fmt.Errorf("writing output file: %w", err)
		}

		errCount := 0
		for _, r := range results {
			if r.Error != "" {
				errCount++
			}
		}
		fmt.Printf("Batch processing complete. Processed %d URLs (%d succeeded, %d failed). Results saved to %s\n",
			len(results), len(results)-errCount, errCount, batchOut)
		return nil
	},
}

func init() {
	batchCmd.Flags().StringVarP(&batchFile, "file", "f", "", "path to a text file containing one URL per line")
	batchCmd.Flags().StringVarP(&batchImpersonate, "impersonate", "i", "chrome", "browser to impersonate (chrome, safari, firefox)")
	batchCmd.Flags().IntVarP(&batchConcurrency, "concurrency", "c", 10, "number of concurrent workers")
	batchCmd.Flags().StringVarP(&batchOut, "out", "o", "results.json", "file to write the output JSON array")
	_ = batchCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(batchCmd)
}
