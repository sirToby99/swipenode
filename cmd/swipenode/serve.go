// cmd/swipenode/serve.go
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/swipenode-local/swipenode/pkg/extractor"
)

var (
	servePort   int
	serveAPIKey string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the SwipeNode REST API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		mux := http.NewServeMux()

		mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})

		mux.HandleFunc("GET /v1/extract", func(w http.ResponseWriter, r *http.Request) {
			// Authentication check.
			if serveAPIKey != "" {
				key := r.Header.Get("X-API-Key")
				if key == "" {
					auth := r.Header.Get("Authorization")
					key = strings.TrimPrefix(auth, "Bearer ")
					if key == auth {
						key = "" // no "Bearer " prefix found
					}
				}
				if key != serveAPIKey {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
					return
				}
			}

			// Extract query parameters.
			url := r.URL.Query().Get("url")
			if url == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "url parameter is required"})
				return
			}

			imp := r.URL.Query().Get("impersonate")
			if imp == "" {
				imp = "chrome"
			}

			// Call extractor.
			data, err := extractor.ExtractData(url, imp)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("failed to extract data: %v", err)})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(data))
		})

		fmt.Printf("Starting SwipeNode REST API on port %d...\n", servePort)
		if serveAPIKey != "" {
			fmt.Println("API key protection is enabled.")
		}

		return http.ListenAndServe(fmt.Sprintf(":%d", servePort), mux)
	},
}

func init() {
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 8080, "port to run the server on")
	serveCmd.Flags().StringVarP(&serveAPIKey, "api-key", "k", "", "API key to protect the server (optional)")
	rootCmd.AddCommand(serveCmd)
}
