package verification

import "testing"

func TestVersionedDocumentContextVerifiesBundledReleaseRelationship(t *testing.T) {
	claim := Claim{Category: "compatibility_claim", Statement: "JetPack 6.2.1 supports Jetson Linux 36.4.4.", VerificationStatus: StatusUnverified}
	evidence := Evidence{
		Source:     "https://docs.nvidia.com/jetson/jetpack/6.2.1/release-notes/index.html",
		SourceType: "official_documentation", Authority: "authoritative", Confidence: 0.85,
		Title: "JetPack 6.2.1 Release Notes — JetPack", VersionDate: "JetPack 6.2.1",
		Content: "NVIDIA Jetson Linux 36.4.4 supports all NVIDIA Jetson Orin modules and developer kits.",
	}
	result := verifyClaim(claim, 0.86, []Evidence{evidence})
	if result.VerificationStatus != StatusVerified {
		t.Fatalf("status=%s reason=%s evidence=%#v", result.VerificationStatus, result.Reason, result.Evidence)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].RetrievedFact == "" {
		t.Fatalf("missing source-backed fact: %#v", result.Evidence)
	}
}

func TestDocumentContextDoesNotVerifyMissingClaimVersion(t *testing.T) {
	claim := Claim{Category: "compatibility_claim", Statement: "JetPack 6.2.1 supports Jetson Linux 99.9.9.", VerificationStatus: StatusUnverified}
	evidence := Evidence{SourceType: "official_documentation", Authority: "authoritative", Confidence: 0.85, Title: "JetPack 6.2.1 Release Notes", VersionDate: "JetPack 6.2.1", Content: "Jetson Linux 36.4.4 supports all Jetson Orin modules."}
	result := verifyClaim(claim, 0.86, []Evidence{evidence})
	if result.VerificationStatus == StatusVerified {
		t.Fatalf("mismatched component release verified: %#v", result)
	}
}
