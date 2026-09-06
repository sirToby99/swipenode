package verification

import (
	"context"
	"testing"
)

func TestCompareTemperatureQuantities(t *testing.T) {
	tests := []struct {
		name  string
		claim string
		fact  string
		want  comparison
	}{
		{
			name:  "different Celsius values conflict",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum operating temperature is 70 °C.",
			want:  comparisonConflicts,
		},
		{
			name:  "Celsius aliases normalize to the same unit",
			claim: "The Acme sensor maximum operating temperature is 70 °C.",
			fact:  "The Acme sensor maximum operating temperature is 70 Celsius.",
			want:  comparisonSupports,
		},
		{
			name:  "plural degrees Celsius prose conflicts with C alias",
			claim: "The Acme gripper maximum operating temperature is 60 C.",
			fact:  "The maximum operating temperature is 50 degrees Celsius (122 degrees Fahrenheit).",
			want:  comparisonConflicts,
		},
		{
			name:  "signed temperature values are extracted",
			claim: "The Acme sensor minimum operating temperature is -20 C.",
			fact:  "The Acme sensor minimum operating temperature is -20 Celsius.",
			want:  comparisonSupports,
		},
		{
			name:  "different temperature properties are not compared",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum storage temperature is 70 °C.",
			want:  comparisonUnknown,
		},
		{
			name:  "unrelated numeric values are ignored",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum storage temperature is 70 °C and its supply voltage is 80 V.",
			want:  comparisonUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := compareClaimAndFact(test.claim, test.fact)
			if got != test.want {
				t.Fatalf("compareClaimAndFact()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestExtractQuantitiesNormalizesPluralDegreesCelsius(t *testing.T) {
	quantities := extractQuantities("The maximum operating temperature is 50 degrees Celsius (122 degrees Fahrenheit).")
	if len(quantities) != 1 {
		t.Fatalf("quantities=%#v, want one supported Celsius quantity", quantities)
	}
	got := quantities[0]
	if got.value != 50 || got.dimension != "temperature" || got.property != "operating_temperature" {
		t.Fatalf("quantity=%#v, want value=50 dimension=temperature property=operating_temperature", got)
	}
}

func TestCompareProtocolIdentifierSyntax(t *testing.T) {
	tests := []struct {
		name  string
		claim string
		fact  string
		want  comparison
	}{
		{
			name:  "equivalent port syntax supports",
			claim: "EXAMPLE/TLS uses default port 443.",
			fact:  "When EXAMPLE/TLS runs over TCP/IP, the default port is 443.",
			want:  comparisonSupports,
		},
		{
			name:  "different port values conflict",
			claim: "EXAMPLE/TLS uses default port 444.",
			fact:  "When EXAMPLE/TLS runs over TCP/IP, the default port is 443.",
			want:  comparisonConflicts,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, _ := compareClaimAndFact(test.claim, test.fact)
			if got != test.want {
				t.Fatalf("compareClaimAndFact()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestAnalyzeTemperatureQuantitiesConservatively(t *testing.T) {
	tests := []struct {
		name  string
		claim string
		fact  string
		want  Status
	}{
		{
			name:  "operating temperature conflict",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum operating temperature is 70 °C.",
			want:  StatusConflict,
		},
		{
			name:  "normalized Celsius verification",
			claim: "The Acme sensor maximum operating temperature is 70 °C.",
			fact:  "The Acme sensor maximum operating temperature is 70 Celsius.",
			want:  StatusVerified,
		},
		{
			name:  "extracted plural degrees Celsius conflict",
			claim: "The Acme gripper maximum operating temperature is 60 C.",
			fact:  "The maximum operating temperature is 50 degrees Celsius (122 degrees Fahrenheit).",
			want:  StatusConflict,
		},
		{
			name:  "signed Celsius verification",
			claim: "The Acme sensor minimum operating temperature is -20 C.",
			fact:  "The Acme sensor minimum operating temperature is -20 Celsius.",
			want:  StatusVerified,
		},
		{
			name:  "storage is not operating temperature",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum storage temperature is 70 °C.",
			want:  StatusUnverified,
		},
		{
			name:  "unrelated numeric value stays ignored",
			claim: "The Acme sensor maximum operating temperature is 80 °C.",
			fact:  "The Acme sensor maximum storage temperature is 70 °C and its supply voltage is 80 V.",
			want:  StatusUnverified,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff := []byte("diff --git a/claims.md b/claims.md\n--- a/claims.md\n+++ b/claims.md\n@@ -0,0 +1 @@\n+" + test.claim + "\n")
			evidence := []Evidence{{
				Source: "fixture://acme-sensor-datasheet", SourceType: "official_datasheet", Authority: "authoritative",
				Confidence: .99, Content: test.fact,
			}}

			report := Analyze(context.Background(), diff, "working_tree", Options{Evidence: evidence})
			if len(report.Claims) != 1 {
				t.Fatalf("claims=%d, want 1: %#v", len(report.Claims), report)
			}
			claim := report.Claims[0]
			if claim.VerificationStatus != test.want {
				t.Fatalf("status=%s, want %s: %#v", claim.VerificationStatus, test.want, claim)
			}
			if len(claim.Evidence) != 1 || claim.Evidence[0].Relevance < minimumRelevance {
				t.Fatalf("result lacks sufficiently relevant evidence: %#v", claim)
			}
		})
	}
}
