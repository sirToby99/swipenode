// cmd/swipenode/mcp.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirToby99/swipenode/internal/buildinfo"
	"github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/pkg/robotics"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the SwipeNode MCP server (stdio transport)",
	RunE: func(cmd *cobra.Command, args []string) error {
		s := server.NewMCPServer("swipenode", buildinfo.Version)
		addExtractTool(s)
		addRoboticsTool(s)
		return server.ServeStdio(s)
	},
}

func addExtractTool(s *server.MCPServer) {
	tool := mcp.NewTool("extract_url",
		mcp.WithDescription("Retrieves public HTTP(S) HTML without JavaScript and returns a structured text-fallback response. It does not guarantee access through WAFs or JavaScript challenges."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The URL to extract data from")),
		mcp.WithString("impersonate", mcp.Description("Compatibility header profile (chrome, safari, firefox; no TLS impersonation)"), mcp.DefaultString("chrome")),
	)

	s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		url, err := request.RequireString("url")
		if err != nil || url == "" {
			return mcp.NewToolResultError("url must be provided as a string"), nil
		}

		result, err := extractor.Extract(ctx, url)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("extraction failed: %v", err)), nil
		}

		respBytes, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError("failed to serialize extraction result"), nil
		}

		return mcp.NewToolResultText(string(respBytes)), nil
	})
}

type findGrippersMCPRequest struct {
	Robot             string   `json:"robot"`
	WorkpieceMassKG   *float64 `json:"workpiece_mass_kg"`
	RequiredOpeningMM *float64 `json:"required_opening_mm"`
	RequiredIPRating  string   `json:"required_ip_rating,omitempty"`
	RequiredInterface string   `json:"required_interface,omitempty"`
	SafetyMarginKG    *float64 `json:"safety_margin_kg,omitempty"`
	AdapterMassKG     *float64 `json:"adapter_mass_kg,omitempty"`
}

func addRoboticsTool(s *server.MCPServer) {
	tool := mcp.NewTool("find_compatible_grippers",
		mcp.WithDescription("Deterministically evaluates the evidence-backed built-in electric-gripper catalog. UNKNOWN and INFERRED data never create PASS."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("robot", mcp.Required(), mcp.Description("Exact robot model or canonical id, currently UR10e")),
		mcp.WithNumber("workpiece_mass_kg", mcp.Required(), mcp.Min(0), mcp.Description("Workpiece mass in kilograms")),
		mcp.WithNumber("required_opening_mm", mcp.Required(), mcp.Min(0), mcp.Description("Required maximum opening in millimeters")),
		mcp.WithString("required_ip_rating", mcp.Description("Optional concrete IP requirement, such as IP54")),
		mcp.WithString("required_interface", mcp.Description("Optional exact communication requirement, such as PROFINET or RS-485")),
		mcp.WithNumber("safety_margin_kg", mcp.Min(0), mcp.Description("Optional explicit payload safety margin in kilograms")),
		mcp.WithNumber("adapter_mass_kg", mcp.Min(0), mcp.Description("Optional adapter mass in kilograms")),
	)
	s.AddTool(tool, handleFindCompatibleGrippers)
}

func handleFindCompatibleGrippers(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input findGrippersMCPRequest
	if err := request.BindArguments(&input); err != nil {
		return mcp.NewToolResultError("invalid arguments: " + err.Error()), nil
	}
	if input.Robot == "" || input.WorkpieceMassKG == nil || input.RequiredOpeningMM == nil {
		return mcp.NewToolResultError("robot, workpiece_mass_kg, and required_opening_mm are required"), nil
	}
	catalog, err := robotics.DefaultCatalog()
	if err != nil {
		return mcp.NewToolResultError("failed to load robotics catalog: " + err.Error()), nil
	}
	response, err := robotics.Evaluate(catalog, robotics.CompatibilityRequest{
		Robot: input.Robot, WorkpieceMassKG: *input.WorkpieceMassKG, RequiredOpeningMM: *input.RequiredOpeningMM,
		RequiredIPRating: input.RequiredIPRating, RequiredInterface: input.RequiredInterface,
		SafetyMarginKG: input.SafetyMarginKG, AdapterMassKG: input.AdapterMassKG,
	})
	if err != nil {
		return mcp.NewToolResultError("compatibility evaluation failed: " + err.Error()), nil
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return mcp.NewToolResultError("failed to serialize compatibility result"), nil
	}
	result := mcp.NewToolResultText(string(encoded))
	result.StructuredContent = response
	return result, nil
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
