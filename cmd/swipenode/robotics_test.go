package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirToby99/swipenode/pkg/robotics"
)

func TestRoboticsCLIJSON(t *testing.T) {
	var output bytes.Buffer
	err := runRoboticsGrippers(&output, roboticsCLIOptions{
		Robot: "UR10e", WorkpieceMassKG: 3.8, RequiredOpeningMM: 90, JSON: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response robotics.CompatibilityResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, output.String())
	}
	if len(response.Candidates) != 3 || response.SchemaVersion != "robotics.compatibility.v1" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestRoboticsCLIHumanOutputExplainsUnknowns(t *testing.T) {
	var output bytes.Buffer
	err := runRoboticsGrippers(&output, roboticsCLIOptions{
		Robot: "UR10e", WorkpieceMassKG: 3.8, RequiredOpeningMM: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"Robotiq 2F-140", "Overall: PASS", "SCHUNK EGU 80-PN-M-B", "Overall: UNKNOWN", "Evidence schunk-egu80"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q:\n%s", expected, text)
		}
	}
}
