// cmd/swipenode/install_mcp.go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

		configPath, err := claudeConfigPath()
		if err != nil {
			return err
		}
		if override, _ := cmd.Flags().GetString("config"); override != "" {
			configPath = override
		}
		configPath, err = filepath.Abs(configPath)
		if err != nil {
			return fmt.Errorf("resolve config path: %w", err)
		}
		claudeDir := filepath.Dir(configPath)

		// Create directory if it doesn't exist.
		if err := os.MkdirAll(claudeDir, 0755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}

		// Read existing config or start fresh.
		var config map[string]interface{}
		data, err := os.ReadFile(configPath)
		if os.IsNotExist(err) || len(data) == 0 {
			config = map[string]interface{}{
				"mcpServers": map[string]interface{}{},
			}
		} else if err != nil {
			return fmt.Errorf("read existing config: %w", err)
		} else {
			if err := json.Unmarshal(data, &config); err != nil {
				return fmt.Errorf("parsing existing config: %w", err)
			}
		}
		if config == nil {
			return fmt.Errorf("parsing existing config: root value must be a JSON object")
		}

		// Ensure mcpServers key exists.
		servers, ok := config["mcpServers"].(map[string]interface{})
		if value, present := config["mcpServers"]; present && !ok {
			return fmt.Errorf("parsing existing config: mcpServers must be a JSON object, not %T", value)
		}
		if !ok {
			servers = map[string]interface{}{}
			config["mcpServers"] = servers
		}

		// Add or update the swipenode entry.
		servers["swipenode"] = map[string]interface{}{
			"command": exePath,
			"args":    []string{"mcp"},
		}

		out, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling config: %w", err)
		}
		out = append(out, '\n')
		backup, err := writeConfigAtomically(configPath, data, out)
		if err != nil {
			return err
		}
		if backup == "" {
			fmt.Printf("Installed SwipeNode MCP server in %s\n", configPath)
		} else {
			fmt.Printf("Installed SwipeNode MCP server in %s (backup: %s)\n", configPath, backup)
		}
		return nil
	},
}

func claudeConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(configDir, "Claude", "claude_desktop_config.json"), nil
}

func writeConfigAtomically(path string, previous, next []byte) (string, error) {
	backup := ""
	if len(previous) > 0 {
		backup = fmt.Sprintf("%s.bak.%s", path, time.Now().UTC().Format("20060102T150405Z"))
		if err := os.WriteFile(backup, previous, 0600); err != nil {
			return "", fmt.Errorf("create configuration backup: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude_desktop_config.*")
	if err != nil {
		return "", fmt.Errorf("create temporary configuration: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("replace configuration atomically: %w", err)
	}
	if len(previous) == 0 {
		return "", nil
	}
	return backup, nil
}

func init() {
	installMcpCmd.Flags().String("config", "", "override Claude Desktop configuration path")
	rootCmd.AddCommand(installMcpCmd)
}
