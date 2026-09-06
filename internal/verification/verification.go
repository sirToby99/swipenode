// Package verification implements SwipeNode's conservative, deterministic
// external-assumption verifier.
package verification

import (
	"bufio"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	webextractor "github.com/sirToby99/swipenode/internal/extractor"
)

const SchemaVersion = "swipenode.verify.v1"

type Status string

const (
	StatusVerified   Status = "VERIFIED"
	StatusConflict   Status = "CONFLICT"
	StatusUnverified Status = "UNVERIFIED"
)

type Location struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type Evidence struct {
	Source        string  `json:"source"`
	SourceType    string  `json:"source_type"`
	Authority     string  `json:"authority"`
	Title         string  `json:"title,omitempty"`
	VersionDate   string  `json:"version_date,omitempty"`
	RetrievedAt   string  `json:"retrieved_at,omitempty"`
	URLReference  string  `json:"url_reference,omitempty"`
	RetrievedFact string  `json:"retrieved_fact,omitempty"`
	Relevance     float64 `json:"relevance"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason"`
	Content       string  `json:"-"`
}

type Claim struct {
	Location           Location   `json:"location"`
	Category           string     `json:"category"`
	Statement          string     `json:"statement"`
	VerificationStatus Status     `json:"verification_status"`
	Evidence           []Evidence `json:"evidence"`
	Confidence         float64    `json:"confidence"`
	Reason             string     `json:"reason"`
}

type Report struct {
	SchemaVersion string   `json:"schema_version"`
	EvidenceMode  string   `json:"evidence_mode"`
	Scope         string   `json:"scope"`
	Clean         bool     `json:"clean"`
	ChangedFiles  []string `json:"changed_files"`
	Claims        []Claim  `json:"claims"`
	Warnings      []string `json:"warnings"`
	ProvenanceID  string   `json:"provenance_id,omitempty"`
}

// Lookup retrieves source-backed evidence for an explicit URL already present
// in a candidate statement. Repository content is never sent to the lookup.
type Lookup interface {
	Lookup(context.Context, string) (Evidence, error)
}

type LookupFunc func(context.Context, string) (Evidence, error)

func (fn LookupFunc) Lookup(ctx context.Context, sourceURL string) (Evidence, error) {
	return fn(ctx, sourceURL)
}

// SwipeNodeLookup adapts the existing hardened public-HTML extractor.
type SwipeNodeLookup struct{}

func (SwipeNodeLookup) Lookup(ctx context.Context, sourceURL string) (Evidence, error) {
	result, err := webextractor.Extract(ctx, sourceURL)
	if err != nil {
		return Evidence{}, err
	}
	return Evidence{
		Source:       result.URL,
		SourceType:   sourceTypeForURL(result.URL),
		Authority:    authorityForURL(result.URL),
		Title:        result.Title,
		RetrievedAt:  result.FetchedAt,
		URLReference: result.URL,
		Confidence:   0.85,
		Content:      result.Content,
	}, nil
}

type Options struct {
	EvidenceMode string
	Evidence     []Evidence
	Lookup       Lookup
}

const (
	EvidenceModeOffline = "offline"
	EvidenceModeNetwork = "network-enabled"
)

var (
	hunkPattern = regexp.MustCompile(`^@@ -[0-9]+(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)
	urlPattern  = regexp.MustCompile(`https?://[^\s<>"'\x60)\]}]+`)

	standardPattern      = regexp.MustCompile(`(?i)\b(RFC\s*\d+|ISO(?:/IEC)?\s*[-0-9:]+|IEEE\s*[0-9.]+|IEC\s*[0-9-]+|standard|specification)\b`)
	compatibilityPattern = regexp.MustCompile(`(?i)\b(compatible|compatibility|supports?|works? with|interoperab|not compatible)\b`)
	hardwarePattern      = regexp.MustCompile(`(?i)\b(payload|voltage|current|stroke|opening|reach|mass|weight|torque|pressure|temperature|IP[0-6][0-9]|kg|mm|cm|mA|amp(?:ere)?s?|volts?|watts?|bar|psi|°C)\b`)
	timeoutPattern       = regexp.MustCompile(`(?i)\b(timeout|deadline|expiry|expires?|TTL|latency|milliseconds?|seconds?|minutes?|hours?)\b`)
	protocolPattern      = regexp.MustCompile(`(?i)\b(protocol|HTTP(?:/\d(?:\.\d)?)?|status code|header|port\s+\d+|TCP|UDP|TLS|macaroon|L402|MCP|RS-?485|PROFINET|MODBUS)\b`)
	versionPattern       = regexp.MustCompile(`(?i)\b(version|release|Go\s+1\.\d+|v?\d+\.\d+(?:\.\d+)?|[A-Za-z][A-Za-z0-9_-]*\s+\d{4})\b`)
	apiPattern           = regexp.MustCompile(`(?i)\b(API|SDK|library|package|module|endpoint|client|server behavior|returns?|accepts?|rejects?)\b`)
	rangePattern         = regexp.MustCompile(`(?i)\b(minimum|maximum|at least|at most|between|range|limit|up to|no more than)\b`)
	vendorPattern        = regexp.MustCompile(`(?i)\b(Alby|Robotiq|SCHUNK|Universal Robots|GitHub|GitLab|Cloudflare|Claude|OpenAI|Entire)\b`)
	sensitiveAssignment  = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|api[_-]?key|authorization)\b\s*[:=]\s*[^\s,;]+`)
)

// Analyze converts a unified git diff into a stable verification report.
func Analyze(ctx context.Context, diff []byte, scope string, options Options) Report {
	mode := options.EvidenceMode
	if mode != EvidenceModeNetwork {
		mode = EvidenceModeOffline
	}
	report := Report{
		SchemaVersion: SchemaVersion,
		EvidenceMode:  mode,
		Scope:         scope,
		Clean:         len(strings.TrimSpace(string(diff))) == 0,
		ChangedFiles:  []string{},
		Claims:        []Claim{},
		Warnings:      []string{},
	}
	if report.Clean {
		return report
	}

	var currentPath string
	newLine := 0
	files := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(string(diff)))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "+++ ") {
			currentPath = parseDiffPath(strings.TrimPrefix(line, "+++ "))
			if currentPath != "" && currentPath != "/dev/null" {
				files[currentPath] = struct{}{}
			}
			continue
		}
		if matches := hunkPattern.FindStringSubmatch(line); matches != nil {
			newLine, _ = strconv.Atoi(matches[1])
			continue
		}
		if currentPath == "" || strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			statement := cleanStatement(strings.TrimPrefix(line, "+"))
			if category, detectionConfidence, ok := classify(semanticClaimText(statement)); ok {
				claim := Claim{
					Location: Location{Path: currentPath, Line: newLine}, Category: category,
					Statement: redact(statement), VerificationStatus: StatusUnverified,
					Evidence: []Evidence{}, Confidence: 0,
					Reason: "No sufficiently strong and relevant evidence was found.",
				}
				candidates := append([]Evidence(nil), options.Evidence...)
				if mode == EvidenceModeNetwork && options.Lookup != nil {
					for _, rawURL := range extractRawURLs(statement) {
						sourceURL, err := SanitizeFetchURL(rawURL)
						if err != nil {
							report.Warnings = append(report.Warnings, "A repository-derived URL was blocked by the network safety policy.")
							continue
						}
						evidence, err := options.Lookup.Lookup(ctx, sourceURL)
						if err != nil {
							report.Warnings = append(report.Warnings, "Evidence retrieval failed for "+sourceURL+": "+safeError(err))
							continue
						}
						if evidence.URLReference == "" {
							evidence.URLReference = sourceURL
						}
						candidates = append(candidates, evidence)
					}
				}
				claim = verifyClaim(claim, detectionConfidence, candidates)
				report.Claims = append(report.Claims, claim)
			}
			newLine++
			continue
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			continue
		}
		if !strings.HasPrefix(line, "\\ No newline") {
			newLine++
		}
	}
	if err := scanner.Err(); err != nil {
		report.Warnings = append(report.Warnings, "Diff parsing stopped before completion: "+err.Error())
	}
	for path := range files {
		report.ChangedFiles = append(report.ChangedFiles, path)
	}
	sort.Strings(report.ChangedFiles)
	return report
}

func classify(statement string) (string, float64, bool) {
	if len(statement) < 12 || isStructuralLine(statement) {
		return "", 0, false
	}
	switch {
	case standardPattern.MatchString(statement):
		return "standards_specification", 0.92, true
	case hardwarePattern.MatchString(statement) && (containsDigit(statement) || rangePattern.MatchString(statement)):
		return "hardware_limit", 0.90, true
	case compatibilityPattern.MatchString(statement):
		return "compatibility_claim", 0.86, true
	case timeoutPattern.MatchString(statement) && containsDigit(statement):
		return "timeout_or_duration", 0.84, true
	case protocolPattern.MatchString(statement):
		return "protocol_constant_or_behavior", 0.82, true
	case rangePattern.MatchString(statement) && containsDigit(statement):
		return "operating_range", 0.80, true
	case versionPattern.MatchString(statement):
		return "version_assumption", 0.78, true
	case apiPattern.MatchString(statement) && (vendorPattern.MatchString(statement) || containsDigit(statement)):
		return "api_or_library_behavior", 0.74, true
	case vendorPattern.MatchString(statement) && (strings.Contains(statement, " is ") || strings.Contains(statement, " uses ")):
		return "vendor_specific_behavior", 0.70, true
	default:
		return "", 0, false
	}
}

func cleanStatement(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"//", "#", "/*", "*", "<!--", "- ", "+ "} {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	value = strings.TrimSuffix(value, "*/")
	value = strings.TrimSuffix(value, "-->")
	value = strings.Join(strings.Fields(value), " ")
	return excerpt(value, 500)
}

// semanticClaimText removes source locators before category detection. URLs
// remain on the reported statement and are still used for explicit retrieval,
// but hostname/path tokens cannot change the meaning of the claim prose.
func semanticClaimText(value string) string {
	return strings.Join(strings.Fields(urlPattern.ReplaceAllString(value, " ")), " ")
}

func redact(value string) string {
	value = sensitiveAssignment.ReplaceAllString(value, "$1=[REDACTED]")
	for _, raw := range urlPattern.FindAllString(value, -1) {
		safe, err := SanitizeFetchURL(strings.TrimRight(raw, ".,;:"))
		if err != nil {
			safe = "[BLOCKED URL]"
		}
		value = strings.ReplaceAll(value, raw, safe)
	}
	return value
}

func extractRawURLs(value string) []string {
	seen := map[string]struct{}{}
	var urls []string
	for _, match := range urlPattern.FindAllString(value, -1) {
		match = strings.TrimRight(match, ".,;:")
		if _, ok := seen[match]; !ok {
			seen[match] = struct{}{}
			urls = append(urls, match)
		}
	}
	return urls
}

func parseDiffPath(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "\"") {
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
	}
	value = strings.TrimPrefix(value, "b/")
	return value
}

func isStructuralLine(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "package ") || strings.HasPrefix(trimmed, "import ") {
		return true
	}
	if strings.HasPrefix(trimmed, "func ") && !strings.Contains(trimmed, "\"") {
		return true
	}
	return trimmed == "{" || trimmed == "}" || trimmed == "}," || trimmed == ")"
}

func containsDigit(value string) bool {
	return strings.IndexFunc(value, unicode.IsDigit) >= 0
}

func excerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
}
