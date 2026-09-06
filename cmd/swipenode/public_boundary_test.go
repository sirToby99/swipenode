package cmd

import "testing"

func TestPublicCustomerCommandBoundary(t *testing.T) {
	for _, name := range []string{"batch", "extract", "install-mcp", "mcp", "knowledge", "audit", "evidence", "verification", "provenance", "robotics", "trust", "ui", "verify"} {
		found, _, err := rootCmd.Find([]string{name})
		if err != nil || found == rootCmd || found.Name() != name {
			t.Fatalf("public customer command %q is unavailable: found=%v err=%v", name, found, err)
		}
	}
	for _, name := range []string{"serve", "knowledge-server", "knowledge-admin", "managed-backup", "publisher", "maintenance"} {
		found, _, err := rootCmd.Find([]string{name})
		if err == nil && found != rootCmd && found.Name() == name {
			t.Fatalf("private command %q crossed the public boundary", name)
		}
	}
}
