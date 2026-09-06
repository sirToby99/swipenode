package provenancecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/spf13/cobra"
)

func TestListAndShowJSON(t *testing.T) {
	state := t.TempDir()
	record, err := provenance.Open(state).Append(provenance.EngineeringProvenanceRecord{
		Timestamp: "2026-09-01T00:00:00Z", Repository: provenance.RepositoryContext{Root: "/repo", GitBranch: "main"},
		Artifacts: []provenance.Artifact{{Path: "bom.yaml", Kind: "file"}}, Verification: provenance.VerificationReference{ID: "vr_test", SchemaVersion: "swipenode.verify.v1", Statuses: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{Getwd: func() (string, error) { return "/repo", nil }, ResolveRoot: func(string) (string, error) { return "/repo", nil }, StateDir: func(context.Context, string) (string, error) { return state, nil }}
	listing := execute(t, NewCommand(deps), "list", "--json")
	var list struct {
		SchemaVersion string                                   `json:"schema_version"`
		Records       []provenance.EngineeringProvenanceRecord `json:"records"`
	}
	if err := json.Unmarshal(listing, &list); err != nil || list.SchemaVersion != "swipenode.provenance-list.v1" || len(list.Records) != 1 {
		t.Fatalf("unexpected list: %v %#v", err, list)
	}
	shown := execute(t, NewCommand(deps), "show", record.ProvenanceID, "--json")
	var decoded provenance.EngineeringProvenanceRecord
	if err := json.Unmarshal(shown, &decoded); err != nil || decoded.ProvenanceID != record.ProvenanceID {
		t.Fatalf("unexpected show: %v %#v", err, decoded)
	}
}

func execute(t *testing.T, command *cobra.Command, args ...string) []byte {
	t.Helper()
	var output bytes.Buffer
	command.SetArgs(args)
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, output.String())
	}
	return output.Bytes()
}
