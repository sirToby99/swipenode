package robotics

import (
	"encoding/json"
	"strings"
	"testing"
)

func defaultRequest() CompatibilityRequest {
	return CompatibilityRequest{Robot: "UR10e", WorkpieceMassKG: 3.8, RequiredOpeningMM: 90}
}

func candidateByID(t *testing.T, response CompatibilityResponse, id string) CandidateResult {
	t.Helper()
	for _, candidate := range response.Candidates {
		if candidate.CandidateID == id {
			return candidate
		}
	}
	t.Fatalf("candidate %s not found", id)
	return CandidateResult{}
}

func checkByName(t *testing.T, candidate CandidateResult, name string) CheckResult {
	t.Helper()
	for _, check := range candidate.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("check %s not found", name)
	return CheckResult{}
}

func TestDefaultCatalogIsEvidenceCompleteAndControlled(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Robots) != 1 || len(catalog.Grippers) != 3 {
		t.Fatalf("catalog sizes = %d robots, %d grippers", len(catalog.Robots), len(catalog.Grippers))
	}
	manufacturers := map[string]bool{}
	for _, gripper := range catalog.Grippers {
		manufacturers[*gripper.Manufacturer.Value] = true
	}
	if len(manufacturers) < 2 {
		t.Fatalf("manufacturers = %v, want at least two", manufacturers)
	}
}

func TestGoldDatasetEndToEndStatuses(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	twoF140 := candidateByID(t, response, "robotiq:2f-140")
	if twoF140.OverallStatus != OverallPass {
		t.Fatalf("2F-140 overall = %s, want PASS; checks=%#v", twoF140.OverallStatus, twoF140.Checks)
	}
	if len(checkByName(t, twoF140, "mechanical").Evidence) == 0 {
		t.Fatal("2F-140 mechanical PASS lacks evidence")
	}
	twoF85 := candidateByID(t, response, "robotiq:2f-85")
	if twoF85.OverallStatus != OverallFail || checkByName(t, twoF85, "opening").Status != CheckFail {
		t.Fatalf("2F-85 should fail opening: %#v", twoF85)
	}
	schunk := candidateByID(t, response, "schunk:egu-80-pn-m-b")
	if schunk.OverallStatus != OverallUnknown || checkByName(t, schunk, "opening").Status != CheckUnknown {
		t.Fatalf("SCHUNK per-jaw stroke must remain UNKNOWN: %#v", schunk)
	}
}

func TestPayloadFailure(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	request := defaultRequest()
	request.WorkpieceMassKG = 20
	response, err := Evaluate(catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range response.Candidates {
		if checkByName(t, candidate, "payload").Status != CheckFail || candidate.OverallStatus != OverallFail {
			t.Fatalf("candidate %s did not fail payload", candidate.CandidateID)
		}
	}
}

func TestAdapterProducesConditionalOnlyWithKnownMass(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	var sourceEvidence []Evidence
	filtered := catalog.Relations[:0]
	for _, relation := range catalog.Relations {
		if relation.Subject == "robotiq:2f-140" {
			sourceEvidence = relation.Evidence
			continue
		}
		filtered = append(filtered, relation)
	}
	catalog.Relations = append(filtered, Relation{
		Subject: "robotiq:2f-140", RelationType: RelationRequiresAdapter,
		Object: "universal-robots:ur10e", Origin: OriginDeclared, Confidence: 0.98,
		Evidence: sourceEvidence, Details: "test adapter",
	})
	adapterMass := 0.2
	request := defaultRequest()
	request.AdapterMassKG = &adapterMass
	response, err := Evaluate(catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateByID(t, response, "robotiq:2f-140")
	if candidate.OverallStatus != OverallConditional || checkByName(t, candidate, "mechanical").Status != CheckAdapterRequired {
		t.Fatalf("adapter result = %#v", candidate)
	}

	request.AdapterMassKG = nil
	response, err = Evaluate(catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	candidate = candidateByID(t, response, "robotiq:2f-140")
	if candidate.OverallStatus != OverallUnknown || checkByName(t, candidate, "payload").Status != CheckUnknown {
		t.Fatalf("missing adapter mass must prevent CONDITIONAL/PASS: %#v", candidate)
	}
}

func TestDeclaredNotCompatibleRelationProducesFail(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Relations {
		if catalog.Relations[index].Subject == "robotiq:2f-140" {
			catalog.Relations[index].RelationType = RelationNotCompatibleWith
		}
	}
	response, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateByID(t, response, "robotiq:2f-140")
	if candidate.OverallStatus != OverallFail || checkByName(t, candidate, "mechanical").Status != CheckFail {
		t.Fatalf("declared incompatibility did not fail: %#v", candidate)
	}
}

func TestInferredRelationCannotCreatePass(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Grippers {
		if catalog.Grippers[index].ID == "robotiq:2f-140" {
			catalog.Grippers[index].MechanicalInterface = unknownText("mechanical_interface", "withheld for inferred-relation test")
		}
	}
	for index := range catalog.Relations {
		if catalog.Relations[index].Subject == "robotiq:2f-140" {
			catalog.Relations[index].Origin = OriginInferred
		}
	}
	response, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateByID(t, response, "robotiq:2f-140")
	if candidate.OverallStatus != OverallUnknown || checkByName(t, candidate, "mechanical").Status != CheckUnknown {
		t.Fatalf("inferred relation produced decisive result: %#v", candidate)
	}
	if len(checkByName(t, candidate, "mechanical").Evidence) == 0 {
		t.Fatal("inferred candidate evidence should remain visible")
	}
}

func TestInferredPropertyCannotCreatePass(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Grippers {
		if catalog.Grippers[index].ID == "robotiq:2f-140" {
			catalog.Grippers[index].OpeningMM.Origin = OriginInferred
		}
	}
	response, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateByID(t, response, "robotiq:2f-140")
	if candidate.OverallStatus != OverallUnknown || checkByName(t, candidate, "opening").Status != CheckUnknown {
		t.Fatalf("inferred property produced PASS: %#v", candidate)
	}
}

func TestMissingEvidenceIsRejected(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Robots[0].PayloadKG.Evidence = nil
	err = catalog.Validate()
	if err == nil || !strings.Contains(err.Error(), "has no evidence") {
		t.Fatalf("missing evidence error = %v", err)
	}
}

func TestRulesAreDeterministic(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	first, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("same catalog and inputs produced different JSON")
	}
}

func TestUnitNormalization(t *testing.T) {
	mass, err := Kilograms(925, "g")
	if err != nil || !nearlyEqual(mass, 0.925) {
		t.Fatalf("925 g = %v kg, err=%v", mass, err)
	}
	length, err := Millimeters(3.5, "in")
	if err != nil || !nearlyEqual(length, 88.9) {
		t.Fatalf("3.5 in = %v mm, err=%v", length, err)
	}
	if _, err := Millimeters(1, "unknown"); err == nil {
		t.Fatal("unknown units must not be guessed")
	}
}

func TestOptionalCommunicationAndEnvironment(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	request := defaultRequest()
	request.RequiredInterface = "RS-485"
	request.RequiredIPRating = "IP40"
	response, err := Evaluate(catalog, request)
	if err != nil {
		t.Fatal(err)
	}
	twoF140 := candidateByID(t, response, "robotiq:2f-140")
	if checkByName(t, twoF140, "communication").Status != CheckPass || checkByName(t, twoF140, "environment").Status != CheckPass {
		t.Fatalf("expected explicit RS-485/IP40 requirements to pass: %#v", twoF140)
	}
	schunk := candidateByID(t, response, "schunk:egu-80-pn-m-b")
	if checkByName(t, schunk, "communication").Status != CheckFail {
		t.Fatalf("SCHUNK must fail an explicit RS-485 requirement: %#v", schunk)
	}
	if checkByName(t, schunk, "environment").Status != CheckUnknown {
		t.Fatalf("component-specific SCHUNK IP ratings must stay UNKNOWN: %#v", schunk)
	}
	if len(checkByName(t, schunk, "environment").Evidence) == 0 {
		t.Fatal("ambiguous SCHUNK IP data should remain traceable")
	}
}

func TestEveryDecisiveDefaultCheckCarriesEvidence(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Evaluate(catalog, defaultRequest())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range response.Candidates {
		for _, check := range candidate.Checks {
			if check.Status != CheckUnknown && len(check.Evidence) == 0 {
				t.Fatalf("%s %s is %s without evidence", candidate.CandidateID, check.Name, check.Status)
			}
		}
	}
}
