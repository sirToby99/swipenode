// cmd/swipenode/mcp.go
package cmd

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"github.com/swipenode-local/swipenode/pkg/extractor"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the SwipeNode MCP server (stdio transport)",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := server.NewMCPServer("swipenode", "1.0.0")

		tool := mcp.NewTool("extract_url",
			mcp.WithDescription("Extracts clean JSON or text from a given URL, automatically bypassing WAFs like Cloudflare using TLS spoofing."),
			mcp.WithString("url", mcp.Required(), mcp.Description("The URL to extract data from")),
			mcp.WithString("impersonate", mcp.Description("Browser to impersonate (chrome, safari, firefox)"), mcp.DefaultString("chrome")),
		)

		s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			url := request.GetString("url", "")
			if url == "" {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent("url is required")},
					IsError: true,
				}, nil
			}

			browser := request.GetString("impersonate", "chrome")

			data, err := extractor.ExtractData(url, browser)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{mcp.NewTextContent(fmt.Sprintf("extraction failed: %v", err))},
					IsError: true,
				}, nil
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{mcp.NewTextContent(data)},
			}, nil
		})

		return server.ServeStdio(s)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
