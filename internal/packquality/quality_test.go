package packquality

import (
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/knowledge"
)

func qualityPack(t *testing.T) knowledge.Pack {
	t.Helper()
	pack, err := knowledge.Parse([]byte(`schema_version: swipenode.knowledge-pack.v1
id: quality-pack
owner: Example
description: Quality test pack.
identities:
  - kind: product
    owner: Example
    name: product
scope:
  topics: [quality]
sources:
  - id: docs
    canonical_url: https://docs.example.com/reference
    authoritative_host: docs.example.com
    source_type: official_documentation
    strategy: explicit_url
    https_required: true
    authoritative: true
    revision: revision-1
trust:
  require_provenance: true
freshness:
  check_strategy: http_metadata_and_content_hash
  suggested_interval: 24h
  revision_strategy: explicit_source_revision
provenance:
  reviewed_at: "2026-09-02"
  reviewed_by: maintainer
  basis: regression review
`), knowledge.OriginManaged)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func completeAssessment() Assessment {
	return Assessment{
		ObservedAt:                 qualityObservationTime(),
		ExpectedSourceIDs:          []string{"docs"},
		RegressionSourceIDs:        []string{"docs"},
		ExtractionSourceIDs:        []string{"docs"},
		SourceContentSHA256:        map[string]string{"docs": strings.Repeat("b", 64)},
		CoveredFacts:               []string{"Product revision compatibility is regression-tested."},
		KnownGaps:                  []string{"Installation behavior is outside this source policy."},
		CoreFactsRegressionCovered: true,
		SourceHealth:               map[string]SourceHealth{"docs": Healthy},
		Release:                    Release{Version: "2026.09.1", Publisher: "SwipeNode", PublisherKeyID: "ed25519:test", PackageSHA256: strings.Repeat("a", 64), SigningMode: "test", SignatureVerified: true},
	}
}

func qualityObservationTime() string {
	return time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
}

func TestQualityReadyOnlyWithExplicitEvidence(t *testing.T) {
	report, err := Evaluate(qualityPack(t), completeAssessment(), time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || !report.ReleaseReady || report.Overall != Pass || report.SchemaVersion != SchemaVersion {
		t.Fatalf("complete quality report: %#v %v", report, err)
	}
	for _, check := range report.Checks {
		if check.Required && check.Status != Pass {
			t.Fatalf("unexpected incomplete check: %#v", check)
		}
	}
}

func TestQualityRejectsStaleObservationAndBadSourceHash(t *testing.T) {
	assessment := completeAssessment()
	assessment.ObservedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	report, err := Evaluate(qualityPack(t), assessment, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || report.ReleaseReady {
		t.Fatalf("stale assessment was accepted: %#v %v", report, err)
	}
	assessment = completeAssessment()
	assessment.SourceContentSHA256["docs"] = "not-a-hash"
	if _, err := Evaluate(qualityPack(t), assessment, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("invalid source hash accepted")
	}
}

func TestQualityDoesNotInventMissingRegressionOrReleaseFacts(t *testing.T) {
	report, err := Evaluate(qualityPack(t), Assessment{}, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || report.ReleaseReady || report.Overall != Warning {
		t.Fatalf("missing facts were treated as ready: %#v %v", report, err)
	}
	for _, check := range report.Checks {
		if check.Dimension == "regression_coverage" && check.Status != NotAssessed {
			t.Fatalf("missing regression facts were not explicit: %#v", check)
		}
	}
}

func TestQualityFlagsDerivedRevisionAndUnhealthySource(t *testing.T) {
	pack := qualityPack(t)
	pack.Sources[0].Revision = ""
	report, err := Evaluate(pack, Assessment{SourceHealth: map[string]SourceHealth{"docs": Unhealthy}}, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if err != nil || report.Overall != Fail || report.ReleaseReady {
		t.Fatalf("unhealthy report: %#v %v", report, err)
	}
	foundRevisionWarning, foundHealthFailure := false, false
	for _, check := range report.Checks {
		foundRevisionWarning = foundRevisionWarning || (check.Dimension == "revision_awareness" && check.Status == Warning)
		foundHealthFailure = foundHealthFailure || (check.Dimension == "release_health" && check.Status == Fail)
	}
	if !foundRevisionWarning || !foundHealthFailure {
		t.Fatalf("expected quality findings missing: %#v", report.Checks)
	}
}

func TestQualityRejectsUnknownAssessmentSource(t *testing.T) {
	if _, err := Evaluate(qualityPack(t), Assessment{RegressionSourceIDs: []string{"unknown"}}, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("unknown assessment source accepted")
	}
}
