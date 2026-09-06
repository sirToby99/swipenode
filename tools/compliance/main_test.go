package main

import (
	"strings"
	"testing"
)

func TestGoSumSHA256AndSPDXID(t *testing.T) {
	digest, err := goSumSHA256("h1:jZ7pwMQXIITcUXNH83LLk+txlaEy6NVOfTuP43xxfqw=")
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	if id := spdxID("github.com/wk8/go-ordered-map/v2"); id != "SPDXRef-Package-github.com-wk8-go-ordered-map-v2" {
		t.Fatalf("SPDX id=%q", id)
	}
	if _, err := goSumSHA256("sha256:bad"); err == nil || !strings.Contains(err.Error(), "h1") {
		t.Fatalf("invalid digest accepted: %v", err)
	}
}

func TestUnresolvedInventoryIssues(t *testing.T) {
	yes := true
	clean := dependency{Module: "example.com/clean", Version: "v1.0.0", License: "MIT", LicenseFiles: []string{"LICENSE"}, CommercialUse: &yes, Modification: &yes, Redistribution: &yes, Risk: "GREEN"}
	if issues := unresolvedInventoryIssues(inventory{Dependencies: []dependency{clean}}); len(issues) != 0 {
		t.Fatalf("clean dependency was rejected: %v", issues)
	}
	unresolved := clean
	unresolved.Module = "example.com/unresolved"
	unresolved.License = "NOASSERTION"
	if issues := unresolvedInventoryIssues(inventory{Dependencies: []dependency{unresolved}}); len(issues) != 1 || !strings.Contains(issues[0], "unresolved") {
		t.Fatalf("unresolved dependency was accepted: %v", issues)
	}
}
