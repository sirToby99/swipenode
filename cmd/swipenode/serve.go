package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/sirToby99/swipenode/pkg/extractor"
)

var port string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start an API server for data extraction",
	Run: func(cmd *cobra.Command, args []string) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/extract", handleExtract)
		mux.Handle("/", http.FileServer(http.Dir("./public")))

		fmt.Printf("Starting server on port %s (localhost only)...\n", port)
		if err := http.ListenAndServe("127.0.0.1:"+port, mux); err != nil {
			fmt.Fprintln(os.Stderr, "Server failed:", err)
			os.Exit(1)
		}
	},
}

func init() {
	serveCmd.Flags().StringVar(&port, "port", "8080", "Port to run the server on")
	rootCmd.AddCommand(serveCmd)
}

type extractRequest struct {
	URL         string `json:"url"`
	File        string `json:"file"`
	Impersonate string `json:"impersonate"`
}

func handleExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req extractRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.File == "" && req.URL == "" {
		http.Error(w, "Either 'url' or 'file' must be provided", http.StatusBadRequest)
		return
	}

	if req.Impersonate == "" {
		req.Impersonate = "chrome" // Default fallback if not provided
	}

	var dataString string
	if req.File != "" {
		dataString, err = extractor.ExtractDataFromFile(req.File)
	} else {
		dataString, err = extractor.ExtractData(req.URL, req.Impersonate)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(dataString))
}
