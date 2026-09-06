package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sirToby99/swipenode/pkg/robotics"
)

func TestFindCompatibleGrippersMCP(t *testing.T) {
	result, err := handleFindCompatibleGrippers(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"robot": "UR10e", "workpiece_mass_kg": 3.8, "required_opening_mm": 90.0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(result.Content) != 1 || result.StructuredContent == nil {
		t.Fatalf("unexpected MCP result: %#v", result)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T", result.Content[0])
	}
	var response robotics.CompatibilityResponse
	if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Candidates) != 3 {
		t.Fatalf("candidate count = %d", len(response.Candidates))
	}
}

func TestFindCompatibleGrippersMCPRejectsMissingInputs(t *testing.T) {
	result, err := handleFindCompatibleGrippers(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"robot": "UR10e"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("missing numeric inputs were accepted: %#v", result)
	}
}
