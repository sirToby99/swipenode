// Package packquality evaluates explicit, auditable readiness gates for
// managed Knowledge Packs. It does not derive a misleading numerical score
// from subjective quality judgments.
package packquality

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/knowledge"
)

const SchemaVersion = "swipenode.pack-quality.v1"

type Status string

const (
	Pass        Status = "pass"
	Warning     Status = "warning"
	Fail        Status = "fail"
	NotAssessed Status = "not_assessed"
)

type SourceHealth string

const (
	Healthy   SourceHealth = "healthy"
	Unhealthy SourceHealth = "unhealthy"
	Unknown   SourceHealth = "unknown"
)

type Release struct {
	Version           string `json:"version,omitempty"`
	Publisher         string `json:"publisher,omitempty"`
	PublisherKeyID    string `json:"publisher_key_id,omitempty"`
	PackageSHA256     string `json:"package_sha256,omitempty"`
	SigningMode       string `json:"signing_mode,omitempty"`
	SignatureVerified bool   `json:"signature_verified"`
}

// Assessment holds operator-observed facts. The evaluator deliberately does
// not pretend that omitted evidence, regression, or health checks succeeded.
type Assessment struct {
	ObservedAt                 string                  `json:"observed_at,omitempty"`
	ExpectedSourceIDs          []string                `json:"expected_source_ids,omitempty"`
	RegressionSourceIDs        []string                `json:"regression_source_ids,omitempty"`
	ExtractionSourceIDs        []string                `json:"extraction_source_ids,omitempty"`
	SourceContentSHA256        map[string]string       `json:"source_content_sha256,omitempty"`
	CoveredFacts               []string                `json:"covered_facts,omitempty"`
	KnownGaps                  []string                `json:"known_gaps,omitempty"`
	CoreFactsRegressionCovered bool                    `json:"core_facts_regression_covered"`
	SourceHealth               map[string]SourceHealth `json:"source_health,omitempty"`
	Release                    Release                 `json:"release"`
}

type Check struct {
	Dimension string `json:"dimension"`
	Required  bool   `json:"required"`
	Status    Status `json:"status"`
	Detail    string `json:"detail"`
}

type Report struct {
	SchemaVersion    string   `json:"schema_version"`
	GeneratedAt      string   `json:"generated_at"`
	PackID           string   `json:"pack_id"`
	ReleaseReady     bool     `json:"release_ready"`
	ProductionSigned bool     `json:"production_signed"`
	Overall          Status   `json:"overall"`
	CoveredFacts     []string `json:"covered_facts"`
	KnownGaps        []string `json:"known_gaps"`
	Checks           []Check  `json:"checks"`
}

func Evaluate(pack knowledge.Pack, assessment Assessment, now time.Time) (Report, error) {
	if err := pack.Validate(); err != nil {
		return Report{}, err
	}
	if now.IsZero() {
		return Report{}, fmt.Errorf("quality report timestamp is required")
	}
	sourceIDs := map[string]bool{}
	for _, source := range pack.Sources {
		sourceIDs[source.ID] = true
	}
	if err := validateAssessment(assessment, sourceIDs); err != nil {
		return Report{}, err
	}

	resolved, err := pack.ResolveSources()
	resolution := Check{Dimension: "deterministic_resolution", Required: true, Status: Pass, Detail: fmt.Sprintf("%d deterministic authoritative endpoint(s) resolved", len(resolved))}
	if err != nil || len(resolved) == 0 {
		resolution.Status, resolution.Detail = Fail, "Pack has no deterministic resolved source endpoint."
	}

	checks := []Check{
		resolution,
		authorityCheck(pack),
		coverageCheck(sourceIDs, assessment.ExpectedSourceIDs),
		revisionCheck(pack),
		freshnessCheck(pack),
		currencyCheck(pack, assessment.ObservedAt, now),
		sourceIntegrityCheck(sourceIDs, assessment.SourceContentSHA256),
		observedSourceCheck("regression_coverage", "source regression", sourceIDs, assessment.RegressionSourceIDs),
		observedSourceCheck("extraction_reliability", "successful extraction", sourceIDs, assessment.ExtractionSourceIDs),
		semanticCheck(assessment.CoreFactsRegressionCovered, assessment.CoveredFacts),
		provenanceCheck(pack),
		healthCheck(sourceIDs, assessment.SourceHealth),
		signatureCheck(pack.Origin, assessment.Release),
	}
	report := Report{SchemaVersion: SchemaVersion, GeneratedAt: now.UTC().Format(time.RFC3339Nano), PackID: pack.ID, ProductionSigned: assessment.Release.SignatureVerified && assessment.Release.SigningMode == "production", CoveredFacts: append([]string(nil), assessment.CoveredFacts...), KnownGaps: append([]string(nil), assessment.KnownGaps...), Checks: checks}
	report.ReleaseReady, report.Overall = readiness(checks)
	return report, nil
}

func validateAssessment(assessment Assessment, sourceIDs map[string]bool) error {
	for _, group := range [][]string{assessment.ExpectedSourceIDs, assessment.RegressionSourceIDs, assessment.ExtractionSourceIDs} {
		for _, id := range group {
			if !sourceIDs[id] {
				return fmt.Errorf("quality assessment refers to unknown source %q", id)
			}
		}
	}
	for id, health := range assessment.SourceHealth {
		if !sourceIDs[id] {
			return fmt.Errorf("quality assessment health refers to unknown source %q", id)
		}
		if health != Healthy && health != Unhealthy && health != Unknown {
			return fmt.Errorf("quality assessment has invalid health %q for source %q", health, id)
		}
	}
	for id, digest := range assessment.SourceContentSHA256 {
		if !sourceIDs[id] {
			return fmt.Errorf("quality assessment hash refers to unknown source %q", id)
		}
		if len(digest) != 64 {
			return fmt.Errorf("quality assessment source hash is not SHA-256")
		}
		if _, err := hex.DecodeString(digest); err != nil || strings.ToLower(digest) != digest {
			return fmt.Errorf("quality assessment source hash is not canonical lowercase SHA-256")
		}
	}
	for _, value := range append(append([]string(nil), assessment.CoveredFacts...), assessment.KnownGaps...) {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("quality assessment facts and gaps must be non-empty")
		}
	}
	return nil
}

func authorityCheck(pack knowledge.Pack) Check {
	for _, source := range pack.Sources {
		if !source.Authoritative || !source.HTTPSRequired {
			return Check{Dimension: "source_authority", Required: true, Status: Fail, Detail: "All managed sources must be authoritative and HTTPS-only."}
		}
	}
	return Check{Dimension: "source_authority", Required: true, Status: Pass, Detail: "All declared sources are authoritative and HTTPS-only."}
}

func coverageCheck(sourceIDs map[string]bool, expected []string) Check {
	if len(expected) == 0 {
		return Check{Dimension: "source_coverage", Required: true, Status: NotAssessed, Detail: "No operator-approved source coverage baseline is recorded."}
	}
	covered := make(map[string]bool, len(expected))
	for _, id := range expected {
		covered[id] = true
	}
	for id := range sourceIDs {
		if !covered[id] {
			return Check{Dimension: "source_coverage", Required: true, Status: Warning, Detail: "The approved coverage baseline does not include every declared source."}
		}
	}
	return Check{Dimension: "source_coverage", Required: true, Status: Pass, Detail: "Every declared source is included in the approved coverage baseline."}
}

func revisionCheck(pack knowledge.Pack) Check {
	for _, source := range pack.Sources {
		if strings.TrimSpace(source.Revision) == "" {
			return Check{Dimension: "revision_awareness", Required: true, Status: Warning, Detail: "At least one source relies on derived revision tracking rather than a declared exact revision."}
		}
	}
	return Check{Dimension: "revision_awareness", Required: true, Status: Pass, Detail: "Every declared source has an explicit revision."}
}

func freshnessCheck(pack knowledge.Pack) Check {
	if pack.Freshness.CheckStrategy == "manual" || strings.TrimSpace(pack.Freshness.SuggestedInterval) == "" {
		return Check{Dimension: "freshness", Required: true, Status: Fail, Detail: "A managed Pack requires an active freshness policy."}
	}
	return Check{Dimension: "freshness", Required: true, Status: Pass, Detail: "An active freshness policy and interval are declared."}
}

func currencyCheck(pack knowledge.Pack, observedAt string, now time.Time) Check {
	if strings.TrimSpace(observedAt) == "" {
		return Check{Dimension: "assessment_currency", Required: true, Status: NotAssessed, Detail: "No timestamp for the current source assessment is recorded."}
	}
	observed, err := time.Parse(time.RFC3339Nano, observedAt)
	if err != nil || observed.After(now.Add(5*time.Minute)) {
		return Check{Dimension: "assessment_currency", Required: true, Status: Fail, Detail: "The source assessment timestamp is invalid or in the future."}
	}
	interval, err := time.ParseDuration(pack.Freshness.SuggestedInterval)
	if err != nil || now.Sub(observed) > interval {
		return Check{Dimension: "assessment_currency", Required: true, Status: Warning, Detail: "The source assessment is older than the Pack freshness interval."}
	}
	return Check{Dimension: "assessment_currency", Required: true, Status: Pass, Detail: "The source assessment is within the declared freshness interval."}
}

func sourceIntegrityCheck(sourceIDs map[string]bool, hashes map[string]string) Check {
	if len(hashes) == 0 {
		return Check{Dimension: "source_integrity", Required: true, Status: NotAssessed, Detail: "No exact source content hash is recorded."}
	}
	for id := range sourceIDs {
		if hashes[id] == "" {
			return Check{Dimension: "source_integrity", Required: true, Status: Warning, Detail: "Not every declared source has an exact content hash."}
		}
	}
	return Check{Dimension: "source_integrity", Required: true, Status: Pass, Detail: "Every declared source has an exact SHA-256 observation."}
}

func observedSourceCheck(dimension, label string, sourceIDs map[string]bool, observed []string) Check {
	if len(observed) == 0 {
		return Check{Dimension: dimension, Required: true, Status: NotAssessed, Detail: "No " + label + " evidence is recorded."}
	}
	present := map[string]bool{}
	for _, id := range observed {
		present[id] = true
	}
	for id := range sourceIDs {
		if !present[id] {
			return Check{Dimension: dimension, Required: true, Status: Warning, Detail: "Not every declared source has " + label + " evidence."}
		}
	}
	return Check{Dimension: dimension, Required: true, Status: Pass, Detail: "Every declared source has " + label + " evidence."}
}

func semanticCheck(covered bool, facts []string) Check {
	if !covered || len(facts) == 0 {
		return Check{Dimension: "semantic_coverage", Required: true, Status: NotAssessed, Detail: "No core-fact semantic regression baseline is recorded."}
	}
	return Check{Dimension: "semantic_coverage", Required: true, Status: Pass, Detail: fmt.Sprintf("%d declared core fact(s) are covered by an explicit semantic regression baseline.", len(facts))}
}

func provenanceCheck(pack knowledge.Pack) Check {
	if !pack.Trust.RequireProvenance || pack.Provenance.ReviewedAt == "" || pack.Provenance.ReviewedBy == "" || pack.Provenance.Basis == "" {
		return Check{Dimension: "provenance_completeness", Required: true, Status: Fail, Detail: "Managed Pack review provenance is incomplete."}
	}
	return Check{Dimension: "provenance_completeness", Required: true, Status: Pass, Detail: "Review provenance is declared."}
}

func healthCheck(sourceIDs map[string]bool, health map[string]SourceHealth) Check {
	if len(health) == 0 {
		return Check{Dimension: "release_health", Required: true, Status: NotAssessed, Detail: "No current source-health observation is recorded."}
	}
	for id := range sourceIDs {
		switch health[id] {
		case Unhealthy:
			return Check{Dimension: "release_health", Required: true, Status: Fail, Detail: "At least one declared source is unhealthy."}
		case Healthy:
		default:
			return Check{Dimension: "release_health", Required: true, Status: NotAssessed, Detail: "At least one declared source has no healthy observation."}
		}
	}
	return Check{Dimension: "release_health", Required: true, Status: Pass, Detail: "Every declared source has a current healthy observation."}
}

func signatureCheck(origin knowledge.Origin, release Release) Check {
	if origin != knowledge.OriginManaged {
		return Check{Dimension: "release_signature", Required: true, Status: NotAssessed, Detail: "The candidate is not an activated managed release."}
	}
	if !release.SignatureVerified || release.Version == "" || release.Publisher == "" || release.PublisherKeyID == "" || len(release.PackageSHA256) != 64 || (release.SigningMode != "test" && release.SigningMode != "production") {
		return Check{Dimension: "release_signature", Required: true, Status: NotAssessed, Detail: "No complete, signature-verified managed release is recorded."}
	}
	if _, err := hex.DecodeString(release.PackageSHA256); err != nil || strings.ToLower(release.PackageSHA256) != release.PackageSHA256 {
		return Check{Dimension: "release_signature", Required: true, Status: Fail, Detail: "The managed release hash is not canonical lowercase SHA-256."}
	}
	return Check{Dimension: "release_signature", Required: true, Status: Pass, Detail: "A complete signed managed release is recorded."}
}

func readiness(checks []Check) (bool, Status) {
	overall := Pass
	for _, check := range checks {
		if check.Required && check.Status != Pass {
			if check.Status == Fail {
				return false, Fail
			}
			overall = Warning
		}
	}
	return overall == Pass, overall
}

// Dimensions returns the stable report ordering for documentation and tools.
func Dimensions(report Report) []string {
	values := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		values = append(values, check.Dimension)
	}
	sort.Strings(values)
	return values
}
