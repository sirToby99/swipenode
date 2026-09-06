package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirToby99/swipenode/internal/entirecmd"
)

func TestExternalBinaryHasPluginOnlyRoot(t *testing.T) {
	command := entirecmd.NewRoot(entirecmd.BuildInfo{Version: "test", Commit: "test", BuildDate: "test"}, entirecmd.DefaultDependencies())
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"entire-swipenode", "context", "doctor", "research", "verify", "init-agents"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("help missing %q:\n%s", expected, output.String())
		}
	}
	for _, forbidden := range []string{"serve", "extract", "mcp", "robotics", "payment", "deploy", "deployment"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("plugin help unexpectedly contains %q:\n%s", forbidden, output.String())
		}
	}
}
