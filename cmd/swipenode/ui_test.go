package cmd

import "testing"

func TestCustomerControlPlaneListenPolicy(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8082", "[::1]:8082"} {
		if err := validateUIListenAddress(address); err != nil {
			t.Fatalf("loopback %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8082", "192.0.2.1:8082", ":8082", "localhost:8082", "127.0.0.1"} {
		if err := validateUIListenAddress(address); err == nil {
			t.Fatalf("unsafe or ambiguous listen address %q accepted", address)
		}
	}
}

func TestCustomerControlPlaneAndRecordCommandsRegistered(t *testing.T) {
	for _, path := range [][]string{{"ui"}, {"evidence", "list"}, {"evidence", "show"}, {"verification", "list"}, {"verification", "show"}} {
		command, _, err := rootCmd.Find(path)
		if err != nil || command == nil || command.Name() != path[len(path)-1] {
			t.Fatalf("standalone command %v missing: command=%v err=%v", path, command, err)
		}
	}
}
