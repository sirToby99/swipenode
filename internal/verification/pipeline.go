package verification

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const minimumRelevance = 0.65

var (
	wordPattern        = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*|\d+(?:\.\d+)?`)
	quantityPattern    = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_.+-])([-+]?(?:\d+(?:\.\d+)?|\.\d+))\s*(°\s*c|degrees?\s+celsius|degrees?\s+c|celsius|milliseconds?|msecs?|ms|seconds?|secs?|minutes?|mins?|hours?|hrs?|kilograms?|kg|grams?|g|millimeters?|mm|centimeters?|cm|meters?|m|milliamps?|ma|amperes?|amps?|a|volts?|v|watts?|w|attempts?|retries?|ports?|c)\b`)
	versionIDPattern   = regexp.MustCompile(`(?i)\b(?:version\s+v?\d+(?:\.\d+){0,2}|v\d+(?:\.\d+){1,2}|\d+\.\d+(?:\.\d+)?)\b`)
	standardIDPattern  = regexp.MustCompile(`(?i)\b(?:RFC\s*\d+|ISO(?:/IEC)?\s*[-0-9:]+|IEEE\s*[0-9.]+|IEC\s*[0-9-]+)\b`)
	portIDPattern      = regexp.MustCompile(`(?i)\bport\s*(?::|=|is)?\s*(\d+)\b`)
	statusCodePattern  = regexp.MustCompile(`(?i)\bstatus\s+code\s*(?::|=|is)?\s*(\d+)\b`)
	httpVersionPattern = regexp.MustCompile(`(?i)\bHTTP/(\d(?:\.\d)?)\b`)
	negativePattern    = regexp.MustCompile(`(?i)\b(?:does not|do not|not|isn't|is not|unsupported|incompatible|cannot|can't)\b`)
	qualifierPattern   = regexp.MustCompile(`(?i)\b(?:minimum|maximum|at least|at most|up to|no more than|limit)\b`)
)

type propertyAlias struct {
	name  string
	terms []string
}

var quantityPropertyAliases = map[string][]propertyAlias{
	"temperature": {
		{name: "operating_temperature", terms: []string{"operating temperature"}},
		{name: "storage_temperature", terms: []string{"storage temperature"}},
		{name: "ambient_temperature", terms: []string{"ambient temperature"}},
		{name: "junction_temperature", terms: []string{"junction temperature"}},
		{name: "surface_temperature", terms: []string{"surface temperature"}},
	},
	"duration": {
		{name: "timeout", terms: []string{"timeout"}},
		{name: "deadline", terms: []string{"deadline"}},
		{name: "expiry", terms: []string{"expiry", "expires", "ttl"}},
		{name: "latency", terms: []string{"latency"}},
		{name: "duration", terms: []string{"duration"}},
	},
	"mass": {
		{name: "payload", terms: []string{"payload"}},
		{name: "mass", terms: []string{"mass"}},
		{name: "weight", terms: []string{"weight"}},
	},
	"length": {
		{name: "stroke", terms: []string{"stroke"}},
		{name: "opening", terms: []string{"opening"}},
		{name: "reach", terms: []string{"reach"}},
		{name: "length", terms: []string{"length"}},
		{name: "distance", terms: []string{"distance"}},
	},
	"current": {
		{name: "current", terms: []string{"current"}},
	},
	"voltage": {
		{name: "voltage", terms: []string{"voltage"}},
	},
	"power": {
		{name: "power", terms: []string{"power"}},
	},
	"attempts": {
		{name: "retry_limit", terms: []string{"retry limit", "retries", "retry", "attempts", "attempt"}},
	},
	"port": {
		{name: "port", terms: []string{"port"}},
	},
}

var stopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "api": {}, "are": {}, "as": {}, "at": {}, "be": {},
	"by": {}, "can": {}, "for": {}, "from": {}, "has": {}, "in": {}, "is": {}, "it": {},
	"of": {}, "on": {}, "or": {}, "our": {}, "service": {}, "system": {}, "the": {}, "this": {},
	"to": {}, "uses": {}, "using": {}, "with": {},
}

var categoryAnchors = map[string][]string{
	"standards_specification":       {"rfc", "iso", "iec", "ieee", "standard", "specification"},
	"hardware_limit":                {"payload", "voltage", "current", "stroke", "opening", "reach", "mass", "weight", "torque", "pressure", "temperature", "limit"},
	"compatibility_claim":           {"compatible", "compatibility", "support", "supports", "works", "interoperable"},
	"timeout_or_duration":           {"timeout", "deadline", "expiry", "expires", "ttl", "latency"},
	"protocol_constant_or_behavior": {"protocol", "http", "status", "header", "port", "tcp", "udp", "tls", "macaroon", "l402", "mcp", "profinet", "modbus"},
	"operating_range":               {"minimum", "maximum", "least", "most", "between", "range", "limit", "up"},
	"version_assumption":            {"version", "release"},
	"api_or_library_behavior":       {"sdk", "library", "package", "module", "endpoint", "client", "server", "returns", "accepts", "rejects"},
	"vendor_specific_behavior":      {"alby", "robotiq", "schunk", "github", "gitlab", "cloudflare", "claude", "openai", "entire"},
}

type comparison int

const (
	comparisonUnknown comparison = iota
	comparisonSupports
	comparisonConflicts
)

func verifyClaim(claim Claim, detectionConfidence float64, candidates []Evidence) Claim {
	type evaluated struct {
		evidence Evidence
		result   comparison
	}
	var evaluatedCandidates []evaluated
	for _, candidate := range candidates {
		fact, relevance := mostRelevantEvidenceFact(claim.Statement, claim.Category, candidate)
		candidate.Relevance = relevance
		candidate.RetrievedFact = excerpt(fact, 320)
		if candidate.Authority == "" {
			candidate.Authority = "unknown"
		}
		if candidate.SourceType == "" {
			candidate.SourceType = "unknown"
		}
		if candidate.Confidence < 0 || candidate.Confidence > 1 {
			candidate.Confidence = 0
		}
		if relevance < minimumRelevance {
			candidate.Reason = "Source text is not sufficiently relevant to the claim."
			evaluatedCandidates = append(evaluatedCandidates, evaluated{candidate, comparisonUnknown})
			continue
		}
		if candidate.Authority != "authoritative" && candidate.Authority != "primary" {
			candidate.Reason = "Relevant text was found, but the source is not classified as authoritative or primary."
			evaluatedCandidates = append(evaluatedCandidates, evaluated{candidate, comparisonUnknown})
			continue
		}
		if candidate.Confidence < 0.75 {
			candidate.Reason = "Relevant text was found, but source confidence is below the deterministic verification threshold."
			evaluatedCandidates = append(evaluatedCandidates, evaluated{candidate, comparisonUnknown})
			continue
		}
		result, reason := compareClaimAndFact(claim.Statement, fact)
		candidate.Reason = reason
		evaluatedCandidates = append(evaluatedCandidates, evaluated{candidate, result})
	}

	sort.SliceStable(evaluatedCandidates, func(i, j int) bool {
		return evaluatedCandidates[i].evidence.Relevance > evaluatedCandidates[j].evidence.Relevance
	})
	var bestSupport, bestConflict *Evidence
	for i := range evaluatedCandidates {
		evidence := evaluatedCandidates[i].evidence
		if evidence.Relevance >= 0.20 && len(claim.Evidence) < 5 {
			claim.Evidence = append(claim.Evidence, evidence)
		}
		switch evaluatedCandidates[i].result {
		case comparisonSupports:
			if bestSupport == nil {
				copy := evidence
				bestSupport = &copy
			}
		case comparisonConflicts:
			if bestConflict == nil {
				copy := evidence
				bestConflict = &copy
			}
		}
	}
	if bestSupport != nil && bestConflict != nil {
		claim.Reason = "Authoritative evidence is inconsistent; both supporting and conflicting facts were found."
		return claim
	}
	if bestConflict != nil {
		claim.VerificationStatus = StatusConflict
		claim.Confidence = combinedConfidence(detectionConfidence, *bestConflict)
		claim.Reason = bestConflict.Reason
		return claim
	}
	if bestSupport != nil {
		claim.VerificationStatus = StatusVerified
		claim.Confidence = combinedConfidence(detectionConfidence, *bestSupport)
		claim.Reason = bestSupport.Reason
		return claim
	}
	return claim
}

// RevalidateClaim applies the existing deterministic comparison pipeline to a
// previously detected claim and a new reviewed evidence set.
func RevalidateClaim(claim Claim, candidates []Evidence) Claim {
	detectionConfidence := 0.80
	if category, confidence, ok := classify(semanticClaimText(claim.Statement)); ok {
		claim.Category = category
		detectionConfidence = confidence
	}
	claim.VerificationStatus = StatusUnverified
	claim.Evidence = []Evidence{}
	claim.Confidence = 0
	claim.Reason = "No sufficiently strong and relevant evidence was found."
	return verifyClaim(claim, detectionConfidence, candidates)
}

func mostRelevantFact(claim, category, content string) (string, float64) {
	var best string
	var bestScore float64
	for _, fact := range splitFacts(content) {
		score := relevanceScore(claim, fact, category)
		if score > bestScore {
			best, bestScore = fact, score
		}
	}
	return best, round(bestScore)
}

// mostRelevantEvidenceFact keeps a fact anchored to the document context that
// gave it meaning. Versioned release-note titles often identify the product
// release while a body sentence identifies the bundled component release. Both
// strings came from the same fetched document; combining them does not invent a
// relationship or consult model knowledge.
func mostRelevantEvidenceFact(claim, category string, evidence Evidence) (string, float64) {
	facts := splitFacts(evidence.Content)
	context := strings.TrimSpace(evidence.Title)
	if evidence.VersionDate != "" && !strings.Contains(strings.ToLower(context), strings.ToLower(evidence.VersionDate)) {
		if context != "" {
			context += ". "
		}
		context += evidence.VersionDate
	}
	var best string
	var bestScore float64
	for _, fact := range facts {
		candidates := []string{fact}
		if context != "" {
			candidates = append(candidates, context+". "+fact)
		}
		for _, candidate := range candidates {
			score := relevanceScore(claim, candidate, category)
			if score > bestScore {
				best, bestScore = candidate, score
			}
		}
	}
	return best, round(bestScore)
}

func splitFacts(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	var facts []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "-*"))
		if line == "" {
			continue
		}
		start := 0
		for i := 0; i < len(line); i++ {
			if (line[i] == '.' || line[i] == '!' || line[i] == '?') && (i+1 == len(line) || line[i+1] == ' ') {
				fact := strings.TrimSpace(line[start : i+1])
				if fact != "" {
					facts = append(facts, fact)
				}
				start = i + 1
			}
		}
		if tail := strings.TrimSpace(line[start:]); tail != "" {
			facts = append(facts, tail)
		}
	}
	return facts
}

func relevanceScore(claim, fact, category string) float64 {
	claimTokens := significantTokens(claim)
	factTokens := significantTokens(fact)
	if len(claimTokens) == 0 || len(factTokens) == 0 {
		return 0
	}
	anchors := categoryAnchors[category]
	anchorShared := false
	for _, anchor := range anchors {
		if hasWord(claim, anchor) && hasWord(fact, anchor) {
			anchorShared = true
			break
		}
	}
	if !anchorShared {
		return 0
	}
	overlap := tokenOverlap(claimTokens, factTokens)
	score := 0.45 + 0.45*overlap
	claimSubject := subjectTokens(claim, anchors)
	if len(claimSubject) > 0 {
		if tokenOverlap(claimSubject, factTokens) == 0 {
			return math.Min(0.45, score)
		}
		score += 0.10
	}
	return math.Min(1, score)
}

func compareClaimAndFact(claim, fact string) (comparison, string) {
	claimQualifier, factQualifier := normalizedQualifier(claim), normalizedQualifier(fact)
	if claimQualifier != factQualifier && (claimQualifier != "" || factQualifier != "") {
		return comparisonUnknown, "Relevant authoritative values were found, but their constraint qualifiers are not equivalent."
	}
	claimQuantities := extractQuantities(claim)
	factQuantities := extractQuantities(fact)
	claimByProperty, factByProperty := quantitiesBySemanticProperty(claimQuantities), quantitiesBySemanticProperty(factQuantities)
	if len(claimByProperty) > 0 {
		allComparable := true
		for _, property := range sortedKeys(claimByProperty) {
			claimValues, factValues := claimByProperty[property], factByProperty[property]
			if len(claimValues) != 1 || len(factValues) != 1 {
				allComparable = false
				continue
			}
			if !nearlyEqual(claimValues[0], factValues[0]) {
				return comparisonConflicts, "Authoritative, relevant evidence explicitly states a different measured value."
			}
		}
		if allComparable {
			return comparisonSupports, "Authoritative, relevant evidence explicitly states the same measured value."
		}
	}
	allIdentifiersComparable := true
	identifierCompared := false
	identifierGroups := [][2][]string{
		{normalizedMatches(standardIDPattern, claim), normalizedMatches(standardIDPattern, fact)},
		{normalizedMatches(versionIDPattern, claim), normalizedMatches(versionIDPattern, fact)},
		{protocolIdentifiers(claim), protocolIdentifiers(fact)},
	}
	for _, identifiers := range identifierGroups {
		claimIDs, factIDs := identifiers[0], identifiers[1]
		if len(claimIDs) == 0 {
			continue
		}
		if len(factIDs) == 0 {
			allIdentifiersComparable = false
			continue
		}
		identifierCompared = true
		if !containsAll(factIDs, claimIDs) {
			if intersects(claimIDs, factIDs) {
				return comparisonUnknown, "Relevant authoritative evidence contains only part of the claimed identifier or version relationship."
			}
			return comparisonConflicts, "Authoritative, relevant evidence explicitly states a different identifier or version."
		}
	}
	if identifierCompared && allIdentifiersComparable {
		return comparisonSupports, "Authoritative, relevant evidence explicitly states the same identifier or version."
	}
	claimNegative, factNegative := negativePattern.MatchString(claim), negativePattern.MatchString(fact)
	if (compatibilityPattern.MatchString(claim) || compatibilityPattern.MatchString(fact)) && claimNegative != factNegative && tokenOverlap(significantTokens(claim), significantTokens(fact)) >= 0.75 {
		return comparisonConflicts, "Authoritative, relevant evidence explicitly has the opposite support or compatibility polarity."
	}
	claimNormalized, factNormalized := normalizeText(claim), normalizeText(fact)
	if claimNormalized != "" && (claimNormalized == factNormalized || strings.Contains(factNormalized, claimNormalized)) {
		return comparisonSupports, "Authoritative, relevant evidence explicitly states the claim."
	}
	return comparisonUnknown, "Relevant authoritative text was found, but no deterministic fact comparison was possible."
}

func containsAll(values, required []string) bool {
	for _, item := range required {
		found := false
		for _, value := range values {
			if value == item {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func normalizedQualifier(value string) string {
	qualifier := strings.ToLower(qualifierPattern.FindString(value))
	switch qualifier {
	case "at most", "up to", "no more than", "maximum":
		return "maximum"
	case "at least", "minimum":
		return "minimum"
	default:
		return qualifier
	}
}

func quantitiesBySemanticProperty(values []quantity) map[string][]float64 {
	result := map[string][]float64{}
	for _, value := range values {
		if value.property == "" {
			continue
		}
		key := value.dimension + "\x00" + value.property
		result[key] = append(result[key], value.value)
	}
	return result
}

func sortedKeys(values map[string][]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type quantity struct {
	value     float64
	dimension string
	property  string
}

func extractQuantities(value string) []quantity {
	var result []quantity
	for _, match := range quantityPattern.FindAllStringSubmatchIndex(value, -1) {
		number, err := strconv.ParseFloat(value[match[2]:match[3]], 64)
		if err != nil {
			continue
		}
		factor, dimension := normalizeUnit(value[match[4]:match[5]])
		property := quantityProperty(value, dimension, match[2])
		result = append(result, quantity{value: number * factor, dimension: dimension, property: property})
	}
	return result
}

func quantityProperty(value, dimension string, numberOffset int) string {
	lower := strings.ToLower(value)
	bestName := ""
	bestDistance := len(lower) + 1
	bestTermLength := 0
	for _, alias := range quantityPropertyAliases[dimension] {
		for _, term := range alias.terms {
			for _, start := range semanticTermOffsets(lower, term) {
				end := start + len(term)
				distance := start - numberOffset
				if end <= numberOffset {
					distance = numberOffset - end
				} else if start <= numberOffset {
					distance = 0
				}
				if distance < bestDistance || (distance == bestDistance && len(term) > bestTermLength) {
					bestName = alias.name
					bestDistance = distance
					bestTermLength = len(term)
				}
			}
		}
	}
	if bestName != "" {
		return bestName
	}
	if dimension == "temperature" && len(semanticTermOffsets(lower, "temperature")) > 0 {
		return "temperature"
	}
	return ""
}

func semanticTermOffsets(value, term string) []int {
	var offsets []int
	for searchFrom := 0; searchFrom < len(value); {
		relative := strings.Index(value[searchFrom:], term)
		if relative < 0 {
			break
		}
		start := searchFrom + relative
		end := start + len(term)
		beforeOK := start == 0 || !isSemanticWordByte(value[start-1])
		afterOK := end == len(value) || !isSemanticWordByte(value[end])
		if beforeOK && afterOK {
			offsets = append(offsets, start)
		}
		searchFrom = start + 1
	}
	return offsets
}

func isSemanticWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}

func normalizeUnit(unit string) (float64, string) {
	unit = strings.ToLower(strings.Join(strings.Fields(unit), " "))
	unit = strings.ReplaceAll(unit, "° ", "°")
	switch {
	case unit == "°c" || unit == "c" || unit == "celsius" ||
		unit == "degree c" || unit == "degrees c" ||
		unit == "degree celsius" || unit == "degrees celsius":
		return 1, "temperature"
	case strings.HasPrefix(unit, "ms") || strings.HasPrefix(unit, "millisecond"):
		return .001, "duration"
	case unit == "s" || strings.HasPrefix(unit, "sec"):
		return 1, "duration"
	case strings.HasPrefix(unit, "min"):
		return 60, "duration"
	case strings.HasPrefix(unit, "h"):
		return 3600, "duration"
	case unit == "kg" || strings.HasPrefix(unit, "kilogram"):
		return 1000, "mass"
	case unit == "g" || strings.HasPrefix(unit, "gram"):
		return 1, "mass"
	case unit == "mm" || strings.HasPrefix(unit, "millimeter"):
		return .001, "length"
	case unit == "cm" || strings.HasPrefix(unit, "centimeter"):
		return .01, "length"
	case unit == "m" || strings.HasPrefix(unit, "meter"):
		return 1, "length"
	case unit == "ma" || strings.HasPrefix(unit, "milliamp"):
		return .001, "current"
	case unit == "a" || strings.HasPrefix(unit, "amp"):
		return 1, "current"
	case unit == "v" || strings.HasPrefix(unit, "volt"):
		return 1, "voltage"
	case unit == "w" || strings.HasPrefix(unit, "watt"):
		return 1, "power"
	case strings.HasPrefix(unit, "attempt") || strings.HasPrefix(unit, "retr"):
		return 1, "attempts"
	case strings.HasPrefix(unit, "port"):
		return 1, "port"
	default:
		return 1, unit
	}
}

func significantTokens(value string) map[string]struct{} {
	value = urlPattern.ReplaceAllString(value, " ")
	result := map[string]struct{}{}
	for _, token := range wordPattern.FindAllString(strings.ToLower(value), -1) {
		if _, stop := stopWords[token]; stop || len(token) < 2 {
			continue
		}
		result[token] = struct{}{}
	}
	return result
}

func subjectTokens(value string, anchors []string) map[string]struct{} {
	lower := strings.ToLower(value)
	cut := len(lower)
	for _, anchor := range anchors {
		if index := strings.Index(lower, anchor); index >= 0 && index < cut {
			cut = index
		}
	}
	result := significantTokens(lower[:cut])
	for token := range result {
		if token == "api" || token == "vendor" || token == "device" {
			delete(result, token)
		}
	}
	return result
}

func tokenOverlap(left, right map[string]struct{}) float64 {
	if len(left) == 0 {
		return 0
	}
	shared := 0
	for token := range left {
		if _, ok := right[token]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(left))
}

func hasWord(value, word string) bool {
	_, ok := significantTokens(value)[strings.ToLower(word)]
	return ok
}

func normalizedMatches(pattern *regexp.Regexp, value string) []string {
	var result []string
	for _, match := range pattern.FindAllString(value, -1) {
		result = append(result, strings.Join(strings.Fields(strings.ToLower(match)), ""))
	}
	return result
}

func protocolIdentifiers(value string) []string {
	var result []string
	for _, definition := range []struct {
		prefix  string
		pattern *regexp.Regexp
	}{
		{prefix: "port:", pattern: portIDPattern},
		{prefix: "status-code:", pattern: statusCodePattern},
		{prefix: "http-version:", pattern: httpVersionPattern},
	} {
		for _, match := range definition.pattern.FindAllStringSubmatch(value, -1) {
			result = append(result, definition.prefix+strings.ToLower(match[1]))
		}
	}
	return result
}

func intersects(left, right []string) bool {
	seen := map[string]struct{}{}
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func normalizeText(value string) string {
	value = urlPattern.ReplaceAllString(value, " ")
	return strings.Join(wordPattern.FindAllString(strings.ToLower(value), -1), " ")
}

func combinedConfidence(detection float64, evidence Evidence) float64 {
	return round(math.Min(detection, math.Min(evidence.Confidence, evidence.Relevance)))
}

func nearlyEqual(left, right float64) bool {
	return math.Abs(left-right) <= math.Max(1e-9, math.Max(math.Abs(left), math.Abs(right))*1e-9)
}

func round(value float64) float64 {
	return math.Round(value*100) / 100
}
