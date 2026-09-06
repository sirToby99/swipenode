package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verificationstore"
)

type fakeFetcher struct {
	document FetchedDocument
	err      error
	calls    int
}

type fakeConditionalFetcher struct {
	documents  []FetchedDocument
	conditions []FetchCondition
	err        error
}

type sourceSequenceFetcher struct {
	documents map[string][]FetchedDocument
}

func (fetcher *sourceSequenceFetcher) Fetch(_ context.Context, target string) (FetchedDocument, error) {
	values := fetcher.documents[target]
	if len(values) == 0 {
		return FetchedDocument{}, errors.New("unexpected source")
	}
	document := values[0]
	if len(values) > 1 {
		fetcher.documents[target] = values[1:]
	}
	return document, nil
}

func (fetcher *fakeConditionalFetcher) Fetch(context.Context, string) (FetchedDocument, error) {
	return fetcher.FetchConditional(context.Background(), "", FetchCondition{})
}

func (fetcher *fakeConditionalFetcher) FetchConditional(_ context.Context, _ string, condition FetchCondition) (FetchedDocument, error) {
	fetcher.conditions = append(fetcher.conditions, condition)
	if fetcher.err != nil {
		return FetchedDocument{}, fetcher.err
	}
	document := fetcher.documents[0]
	if len(fetcher.documents) > 1 {
		fetcher.documents = fetcher.documents[1:]
	}
	return document, nil
}

func (f *fakeFetcher) Fetch(context.Context, string) (FetchedDocument, error) {
	f.calls++
	return f.document, f.err
}

func TestRefreshOfflinePerformsNoNetwork(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{err: errors.New("must not be called")}
	report, err := (RefreshService{Fetcher: fetcher}).Refresh(context.Background(), pack, false)
	if err != nil {
		t.Fatal(err)
	}
	if fetcher.calls != 0 || report.FetchEnabled || len(report.Results) != 1 || report.Results[0].Status != "offline" {
		t.Fatalf("unexpected offline refresh: %#v, calls=%d", report, fetcher.calls)
	}
}

func TestRefreshTracksUnchangedChangedAndRevision(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	evidence := evidencestore.Open(dir)
	auditor := audit.Open(dir)
	fetcher := &fakeFetcher{document: FetchedDocument{Content: "version one", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"one"`}}
	service := RefreshService{Evidence: evidence, Audit: auditor, Fetcher: fetcher, Now: func() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC) }}
	first, err := service.Refresh(context.Background(), pack, true)
	if err != nil || first.Changed != 1 {
		t.Fatalf("first refresh = %#v, %v", first, err)
	}
	fetcher.document.FetchedAt = "2026-09-01T11:00:00Z"
	second, err := service.Refresh(context.Background(), pack, true)
	if err != nil || second.Unchanged != 1 {
		t.Fatalf("unchanged refresh = %#v, %v", second, err)
	}
	fetcher.document.Content, fetcher.document.FetchedAt = "version two", "2026-09-01T12:00:00Z"
	third, err := service.Refresh(context.Background(), pack, true)
	if err != nil || third.Changed != 1 || third.Results[0].OldHash == third.Results[0].NewHash {
		t.Fatalf("changed refresh = %#v, %v", third, err)
	}
	pack.Sources[0].Revision = "revision-2"
	fetcher.document.FetchedAt = "2026-09-01T13:00:00Z"
	fourth, err := service.Refresh(context.Background(), pack, true)
	if err != nil || fourth.Changed != 1 {
		t.Fatalf("revision refresh = %#v, %v", fourth, err)
	}
	records, err := evidence.List()
	if err != nil || len(records) != 3 {
		t.Fatalf("evidence history = %d, %v", len(records), err)
	}
	events, err := auditor.List()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[audit.EventType]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts[audit.SourceChecked] != 4 || counts[audit.SourceUnchanged] != 1 || counts[audit.SourceChanged] != 2 || counts[audit.EvidenceCreated] != 3 {
		t.Fatalf("audit counts = %#v", counts)
	}
	if fourth.Results[0].CheckMethod != "source_revision" || fourth.Results[0].OldRevision == fourth.Results[0].NewRevision {
		t.Fatalf("explicit revision transition = %#v", fourth.Results[0])
	}
}

func TestRefreshReportsFetchFailure(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := (RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: &fakeFetcher{err: errors.New("network unavailable")}}).Refresh(context.Background(), pack, true)
	if err == nil || report.Failed != 1 || report.Results[0].Error != "network unavailable" {
		t.Fatalf("failure report = %#v, %v", report, err)
	}
}

func TestM05PackRefreshFailureSurfaced(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("managed-pack", "https://docs.example.com/reference")), OriginManaged)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	report, err := (RefreshService{
		Evidence: evidencestore.Open(dir),
		Audit:    audit.Open(dir),
		Fetcher:  &fakeFetcher{err: FetchFailure{Kind: "timeout", Err: context.DeadlineExceeded}},
	}).Refresh(context.Background(), pack, true)
	if err == nil || report.Failed != 1 || len(report.Results) != 1 || report.Results[0].FailureKind != "timeout" || report.Results[0].Status != "failed" {
		t.Fatalf("managed refresh failure was not surfaced: report=%#v err=%v", report, err)
	}
}

func TestFetchFailureClassification(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"disappeared", FetchFailure{Kind: "source_disappeared", Err: errors.New("gone")}, "source_disappeared"},
		{"parser", FetchFailure{Kind: "parser_failure", Err: errors.New("parse")}, "parser_failure"},
		{"network", errors.New("connection refused"), "network_failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyFetchFailure(test.err); got != test.want {
				t.Fatalf("classification = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRefreshReportsPathOnlySourceAsUnresolved(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	pack.Sources[0].Strategy = "path_pattern"
	pack.Sources[0].PathPattern = "/reference/*"
	report, err := (RefreshService{}).Refresh(context.Background(), pack, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != "unresolved" {
		t.Fatalf("expected explicit unresolved result: %#v", report)
	}
}

func TestConditional304UpdatesOnlyFreshnessState(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	pack.Freshness.CheckStrategy = "http_metadata_and_content_hash"
	dir := t.TempDir()
	fetcher := &fakeConditionalFetcher{documents: []FetchedDocument{{Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"v1"`, LastModified: "Tue, 01 Sep 2026 10:00:00 GMT"}, {FetchedAt: "2026-09-01T11:00:00Z", ETag: `"v1"`, NotModified: true}}}
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher}
	if report, err := service.Refresh(context.Background(), pack, true); err != nil || report.Changed != 1 {
		t.Fatalf("initial refresh: %#v %v", report, err)
	}
	before, _ := service.Evidence.List()
	report, err := service.Refresh(context.Background(), pack, true)
	if err != nil || report.Unchanged != 1 || report.Results[0].CheckMethod != "http_304" {
		t.Fatalf("conditional refresh: %#v %v", report, err)
	}
	if len(fetcher.conditions) != 2 || fetcher.conditions[1].ETag != `"v1"` || fetcher.conditions[1].LastModified == "" {
		t.Fatalf("validators not sent: %#v", fetcher.conditions)
	}
	after, _ := service.Evidence.List()
	state, ok, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	if len(before) != len(after) || !ok || state.LastChecked != "2026-09-01T11:00:00Z" || state.Status != "unchanged" {
		t.Fatalf("304 mutated evidence or missed state update: before=%d after=%d state=%#v", len(before), len(after), state)
	}
}

func TestRefreshFailurePreservesKnownGoodEvidence(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	dir := t.TempDir()
	fetcher := &fakeConditionalFetcher{documents: []FetchedDocument{{Content: "known good", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"v1"`}}}
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher, Now: func() time.Time { return time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC) }}
	if _, err := service.Refresh(context.Background(), pack, true); err != nil {
		t.Fatal(err)
	}
	before, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	fetcher.err = FetchFailure{Kind: "timeout", Err: context.DeadlineExceeded}
	report, err := service.Refresh(context.Background(), pack, true)
	if err == nil || report.Results[0].FailureKind != "timeout" {
		t.Fatalf("failure classification: %#v %v", report, err)
	}
	after, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	if after.CurrentEvidenceID != before.CurrentEvidenceID || after.ContentSHA256 != before.ContentSHA256 || after.Health != "unhealthy" || after.Status != "timeout" {
		t.Fatalf("known-good state was lost: before=%#v after=%#v", before, after)
	}
}

func TestF10InvalidContentPreservesKnownGoodEvidence(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	dir := t.TempDir()
	fetcher := &fakeConditionalFetcher{documents: []FetchedDocument{{Content: "known good", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"v1"`}}}
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher, Now: func() time.Time { return time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC) }}
	if _, err := service.Refresh(context.Background(), pack, true); err != nil {
		t.Fatal(err)
	}
	before, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	fetcher.err = FetchFailure{Kind: "invalid_content", Err: errors.New("document contains no usable technical content")}
	report, err := service.Refresh(context.Background(), pack, true)
	if err == nil || report.Results[0].FailureKind != "invalid_content" {
		t.Fatalf("invalid-content classification: %#v %v", report, err)
	}
	after, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	if after.CurrentEvidenceID != before.CurrentEvidenceID || after.ContentSHA256 != before.ContentSHA256 || after.Health != "unhealthy" || after.Status != "invalid_content" {
		t.Fatalf("invalid content replaced known-good state: before=%#v after=%#v", before, after)
	}
}

func TestRefreshDryRunDoesNotMutateStores(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	dir := t.TempDir()
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: &fakeFetcher{document: FetchedDocument{Content: "new content", FetchedAt: "2026-09-01T10:00:00Z"}}}
	report, err := service.RefreshWithOptions(context.Background(), pack, RefreshOptions{Fetch: true, DryRun: true})
	if err != nil || report.Changed != 1 || !report.Results[0].WouldChange {
		t.Fatalf("dry run report: %#v %v", report, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry run mutated state: %#v", entries)
	}
}

func TestRefreshRevalidationTransitionsAndProvenance(t *testing.T) {
	for _, test := range []struct {
		name, oldFact, newFact, oldStatus, newStatus string
		statusChanges                                int
	}{
		{"verified-to-conflict", "Example API timeout is 10 seconds.", "Example API timeout is 20 seconds.", "VERIFIED", "CONFLICT", 1},
		{"conflict-to-verified", "Example API timeout is 20 seconds.", "Example API timeout is 10 seconds.", "CONFLICT", "VERIFIED", 1},
		{"verified-remains-verified", "Example API timeout is 10 seconds.", "Example API timeout is 10 seconds. This applies to all clients.", "VERIFIED", "VERIFIED", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
			dir := t.TempDir()
			fetcher := &fakeConditionalFetcher{documents: []FetchedDocument{{Content: test.oldFact, FetchedAt: "2026-09-01T10:00:00Z"}, {Content: test.newFact, FetchedAt: "2026-09-01T11:00:00Z"}}}
			service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher}
			if _, err := service.Refresh(context.Background(), pack, true); err != nil {
				t.Fatal(err)
			}
			state, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
			oldEvidence, ok, _ := service.Evidence.Find(state.CurrentEvidenceID)
			if !ok {
				t.Fatal("missing initial evidence")
			}
			verifications := verificationstore.Open(dir)
			oldVerification, err := verifications.Append(verificationstore.Record{RecordedAt: "2026-09-01T10:30:00Z", ReportID: "vr_initial", PackID: pack.ID, Scope: "working_tree", Repository: verificationstore.Repository{Root: "/repo", GitCommit: strings.Repeat("a", 40), GitBranch: "main"}, Artifacts: []string{"config.yaml"}, Claims: []verificationstore.Claim{{ArtifactPath: "config.yaml", Line: 2, Category: "timeout_or_duration", Statement: "Example API timeout is 10 seconds.", Status: test.oldStatus, Evidence: []verificationstore.EvidenceReference{{EvidenceID: oldEvidence.ID, PackID: pack.ID, SourceID: "docs", CanonicalURL: oldEvidence.CanonicalURL, SourceType: oldEvidence.SourceType, Authority: "authoritative", Confidence: 0.95, RetrievedFact: test.oldFact, ContentSHA256: oldEvidence.ContentSHA256}}}}})
			if err != nil {
				t.Fatal(err)
			}
			service.Verification, service.Provenance = verifications, provenance.Open(dir)
			service.Repository = provenance.RepositoryContext{Root: "/repo", GitCommit: strings.Repeat("a", 40), GitBranch: "main"}
			report, err := service.RefreshWithOptions(context.Background(), pack, RefreshOptions{Fetch: true, Revalidate: true})
			if err != nil || report.AffectedVerifications != 1 || report.Revalidated != 1 || len(report.StatusChanges) != test.statusChanges {
				t.Fatalf("revalidation report: %#v %v", report, err)
			}
			if test.statusChanges == 1 && (report.StatusChanges[0].OldStatus != test.oldStatus || report.StatusChanges[0].NewStatus != test.newStatus) {
				t.Fatalf("status transition: %#v", report.StatusChanges)
			}
			records, _ := verifications.List()
			childFound := false
			for _, record := range records {
				childFound = childFound || record.ParentVerificationID == oldVerification.ID
			}
			if len(records) != 2 || !childFound {
				t.Fatalf("verification history was overwritten: %#v", records)
			}
			provenanceRecords, _ := service.Provenance.List()
			if len(provenanceRecords) != test.statusChanges {
				t.Fatalf("transition provenance missing: %#v", provenanceRecords)
			}
			if test.statusChanges == 1 && (len(provenanceRecords[0].TruthTransitions) != 1 || provenanceRecords[0].TruthTransitions[0].OldStatus != test.oldStatus || provenanceRecords[0].TruthTransitions[0].NewStatus != test.newStatus) {
				t.Fatalf("transition provenance is incorrect: %#v", provenanceRecords)
			}
			events, err := service.Audit.List()
			if err != nil {
				t.Fatal(err)
			}
			counts := map[audit.EventType]int{}
			for _, event := range events {
				counts[event.Type]++
			}
			if counts[audit.RevalidationStarted] != 1 || counts[audit.VerificationRevalidated] != 1 || counts[audit.VerificationStatusChanged] != test.statusChanges {
				t.Fatalf("revalidation audit counts: %#v", counts)
			}
			if _, err := os.Stat(filepath.Join(dir, "audit.jsonl")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRevalidationRetainsOtherSourceDependencies(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	pack.Sources = append(pack.Sources, Source{ID: "support", CanonicalURL: "https://docs.example.com/support", AuthoritativeHost: "docs.example.com", SourceType: "vendor_support", Strategy: "explicit_url", HTTPSRequired: true, Authoritative: true})
	dir := t.TempDir()
	fetcher := &sourceSequenceFetcher{documents: map[string][]FetchedDocument{
		"https://docs.example.com/reference": {{Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z"}, {Content: "Example API timeout is 20 seconds.", FetchedAt: "2026-09-01T11:00:00Z"}},
		"https://docs.example.com/support":   {{Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z"}, {Content: "Example API timeout is 20 seconds.", FetchedAt: "2026-09-01T11:00:00Z"}},
	}}
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher}
	if _, err := service.Refresh(context.Background(), pack, true); err != nil {
		t.Fatal(err)
	}
	references := []verificationstore.EvidenceReference{}
	for _, source := range []string{"docs", "support"} {
		state, ok, err := service.Evidence.SourceState(pack.ID, source, "https://docs.example.com/"+map[string]string{"docs": "reference", "support": "support"}[source])
		if err != nil || !ok {
			t.Fatalf("source state %s: %#v %t %v", source, state, ok, err)
		}
		record, ok, err := service.Evidence.Find(state.CurrentEvidenceID)
		if err != nil || !ok {
			t.Fatalf("evidence %s: %#v %t %v", source, record, ok, err)
		}
		references = append(references, verificationstore.EvidenceReference{EvidenceID: record.ID, PackID: pack.ID, SourceID: source, CanonicalURL: record.CanonicalURL, SourceType: record.SourceType, Authority: "authoritative", Confidence: 0.95, RetrievedFact: "Example API timeout is 10 seconds.", ContentSHA256: record.ContentSHA256})
	}
	verifications := verificationstore.Open(dir)
	if _, err := verifications.Append(verificationstore.Record{RecordedAt: "2026-09-01T10:30:00Z", ReportID: "vr_initial", PackID: pack.ID, Scope: "working_tree", Repository: verificationstore.Repository{Root: "/repo"}, Artifacts: []string{"config.yaml"}, Claims: []verificationstore.Claim{{ArtifactPath: "config.yaml", Line: 1, Category: "timeout_or_duration", Statement: "Example API timeout is 10 seconds.", Status: "VERIFIED", Evidence: references}}}); err != nil {
		t.Fatal(err)
	}
	service.Verification, service.Provenance = verifications, provenance.Open(dir)
	service.Repository = provenance.RepositoryContext{Root: "/repo"}
	report, err := service.RefreshWithOptions(context.Background(), pack, RefreshOptions{Fetch: true, Revalidate: true})
	if err != nil || report.Changed != 2 || report.AffectedVerifications != 2 || report.Revalidated != 2 {
		t.Fatalf("multi-source refresh: %#v %v", report, err)
	}
	latest, err := verifications.LatestDependents(pack.ID, "support")
	if err != nil || len(latest) != 1 || len(latest[0].Claims[0].Evidence) != 2 {
		t.Fatalf("other dependency was lost: %#v %v", latest, err)
	}
}

func TestRevalidationLinksEvidenceUsedByOldVerification(t *testing.T) {
	pack, _ := Parse([]byte(testPackYAML("refresh-pack", "https://docs.example.com/reference")), OriginProject)
	dir := t.TempDir()
	fetcher := &fakeConditionalFetcher{documents: []FetchedDocument{
		{Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z"},
		{Content: "Example API timeout is 10 seconds. Documentation layout changed.", FetchedAt: "2026-09-01T11:00:00Z"},
		{Content: "Example API timeout is 20 seconds.", FetchedAt: "2026-09-01T12:00:00Z"},
	}}
	service := RefreshService{Evidence: evidencestore.Open(dir), Audit: audit.Open(dir), Fetcher: fetcher}
	if _, err := service.Refresh(context.Background(), pack, true); err != nil {
		t.Fatal(err)
	}
	firstState, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	firstEvidence, ok, _ := service.Evidence.Find(firstState.CurrentEvidenceID)
	if !ok {
		t.Fatal("missing first evidence")
	}
	verifications := verificationstore.Open(dir)
	if _, err := verifications.Append(verificationstore.Record{RecordedAt: "2026-09-01T10:30:00Z", ReportID: "vr_initial", PackID: pack.ID, Scope: "working_tree", Repository: verificationstore.Repository{Root: "/repo"}, Artifacts: []string{"config.yaml"}, Claims: []verificationstore.Claim{{ArtifactPath: "config.yaml", Line: 1, Category: "timeout_or_duration", Statement: "Example API timeout is 10 seconds.", Status: "VERIFIED", Evidence: []verificationstore.EvidenceReference{{EvidenceID: firstEvidence.ID, PackID: pack.ID, SourceID: "docs", CanonicalURL: firstEvidence.CanonicalURL, SourceType: firstEvidence.SourceType, Authority: "authoritative", Confidence: 0.95, RetrievedFact: "Example API timeout is 10 seconds.", ContentSHA256: firstEvidence.ContentSHA256}}}}}); err != nil {
		t.Fatal(err)
	}
	service.Verification, service.Provenance = verifications, provenance.Open(dir)
	service.Repository = provenance.RepositoryContext{Root: "/repo"}
	if report, err := service.Refresh(context.Background(), pack, true); err != nil || report.Changed != 1 || report.AffectedVerifications != 1 {
		t.Fatalf("intermediate source version: %#v %v", report, err)
	}
	secondState, _, _ := service.Evidence.SourceState(pack.ID, "docs", "https://docs.example.com/reference")
	if secondState.CurrentEvidenceID == firstEvidence.ID {
		t.Fatal("intermediate source version was not stored")
	}
	report, err := service.RefreshWithOptions(context.Background(), pack, RefreshOptions{Fetch: true, Revalidate: true})
	if err != nil || len(report.StatusChanges) != 1 {
		t.Fatalf("third source revalidation: %#v %v", report, err)
	}
	records, err := service.Provenance.List()
	if err != nil || len(records) != 1 {
		t.Fatalf("provenance history: %#v %v", records, err)
	}
	transition := records[0].TruthTransitions[0]
	if transition.OldEvidenceID != firstEvidence.ID || transition.OldEvidenceID == secondState.CurrentEvidenceID || transition.OldHash != firstEvidence.ContentSHA256 {
		t.Fatalf("transition linked source state instead of verification evidence: %#v", transition)
	}
	events, _ := service.Audit.List()
	found := false
	for _, event := range events {
		if event.Type == audit.VerificationStatusChanged {
			found = event.OldEvidenceID == firstEvidence.ID && event.OldHash == firstEvidence.ContentSHA256
		}
	}
	if !found {
		t.Fatal("status-change audit did not link the old verification evidence")
	}
}
