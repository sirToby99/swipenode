// cmd/swipenode/install_mcp.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var installMcpCmd = &cobra.Command{
	Use:   "install-mcp",
	Short: "Install SwipeNode as an MCP server in Claude Desktop config",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get the absolute path of the current executable.
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("getting executable path: %w", err)
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return fmt.Errorf("resolving absolute path: %w", err)
		}

		// Locate the Claude Desktop config directory.
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("getting config directory: %w", err)
		}
		claudeDir := filepath.Join(configDir, "Claude")
		configPath := filepath.Join(claudeDir, "claude_desktop_config.json")

		// Create directory if it doesn't exist.
		if err := os.MkdirAll(claudeDir, 0755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		// Read existing config or start fresh.
		var config map[string]interface{}
		data, err := os.ReadFile(configPath)
		if err != nil || len(data) == 0 {
			config = map[string]interface{}{
				"mcpServers": map[string]interface{}{},
			}
		} else {
			if err := json.Unmarshal(data, &config); err != nil {
				return fmt.Errorf("parsing existing config: %w", err)
			}
		}

		// Ensure mcpServers key exists.
		servers, ok := config["mcpServers"].(map[string]interface{})
		if !ok {
			servers = map[string]interface{}{}
			config["mcpServers"] = servers
		}

		// Add or update the swipenode entry.
		servers["swipenode"] = map[string]interface{}{
			"command": exePath,
			"args":    []string{"mcp"},
		}

		// Write back.
		out, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling config: %w", err)
		}
		if err := os.WriteFile(configPath, out, 0644); err != nil {
			return fmt.Errorf("writing config: %w", err)
		}

		fmt.Printf("Successfully installed SwipeNode MCP server to Claude Desktop config at %s\n", configPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installMcpCmd)
}
