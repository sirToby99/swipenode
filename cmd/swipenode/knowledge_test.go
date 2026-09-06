package cmd

import "testing"

func TestStandaloneKnowledgeAndAuditCommandsRegistered(t *testing.T) {
	for _, path := range [][]string{{"knowledge", "list"}, {"knowledge", "show"}, {"knowledge", "validate"}, {"knowledge", "refresh"}, {"audit", "list"}, {"audit", "show"}, {"evidence", "list"}, {"evidence", "show"}, {"verification", "list"}, {"verification", "show"}} {
		command, _, err := rootCmd.Find(path)
		if err != nil || command == nil || command.Name() != path[len(path)-1] {
			t.Fatalf("standalone command %v missing: command=%v err=%v", path, command, err)
		}
	}
	verify, _, err := rootCmd.Find([]string{"verify"})
	if err != nil || verify.Flags().Lookup("knowledge-pack") == nil {
		t.Fatalf("standalone verify missing --knowledge-pack: %v", err)
	}
}
