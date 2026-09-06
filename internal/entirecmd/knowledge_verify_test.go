package entirecmd

import "testing"

func TestEntireVerifyExposesSharedKnowledgePackFlag(t *testing.T) {
	command := newVerifyCommand(DefaultDependencies())
	for _, flag := range []string{"knowledge-pack", "record-provenance"} {
		if command.Flags().Lookup(flag) == nil {
			t.Fatalf("entire-swipenode verify is missing shared --%s flag", flag)
		}
	}
}

func TestEntireRootExposesSharedProvenanceCommands(t *testing.T) {
	root := NewRoot(BuildInfo{Version: "test"}, DefaultDependencies())
	command, _, err := root.Find([]string{"provenance", "list"})
	if err != nil || command.Name() != "list" {
		t.Fatalf("provenance list is not exposed: %v %v", command, err)
	}
}
