package robotics

import (
	"fmt"
	"sort"
	"strings"
)

const (
	RulePayloadVersion       = "1.0.0"
	RuleOpeningVersion       = "1.0.0"
	RuleMechanicalVersion    = "1.0.0"
	RuleElectricalVersion    = "1.0.0"
	RuleCommunicationVersion = "1.0.0"
	RuleEnvironmentVersion   = "1.0.0"
)

// CheckStatus is a deterministic rule result. ADAPTER_REQUIRED is confined to
// the mechanical check and maps to overall CONDITIONAL when all other checks pass.
type CheckStatus string

const (
	CheckPass            CheckStatus = "PASS"
	CheckFail            CheckStatus = "FAIL"
	CheckUnknown         CheckStatus = "UNKNOWN"
	CheckAdapterRequired CheckStatus = "ADAPTER_REQUIRED"
)

// OverallStatus is the only top-level compatibility vocabulary.
type OverallStatus string

const (
	OverallPass        OverallStatus = "PASS"
	OverallConditional OverallStatus = "CONDITIONAL"
	OverallFail        OverallStatus = "FAIL"
	OverallUnknown     OverallStatus = "UNKNOWN"
)

// CompatibilityRequest contains only explicit user requirements. Optional
// numeric inputs remain pointers so omission cannot be confused with zero.
type CompatibilityRequest struct {
	Robot             string   `json:"robot"`
	WorkpieceMassKG   float64  `json:"workpiece_mass_kg"`
	RequiredOpeningMM float64  `json:"required_opening_mm"`
	RequiredIPRating  string   `json:"required_ip_rating,omitempty"`
	RequiredInterface string   `json:"required_interface,omitempty"`
	SafetyMarginKG    *float64 `json:"safety_margin_kg,omitempty"`
	AdapterMassKG     *float64 `json:"adapter_mass_kg,omitempty"`
}

// AppliedInputs makes default-zero optional inputs visible in machine output.
type AppliedInputs struct {
	WorkpieceMassKG     float64 `json:"workpiece_mass_kg"`
	RequiredOpeningMM   float64 `json:"required_opening_mm"`
	SafetyMarginKG      float64 `json:"safety_margin_kg"`
	AdapterMassKG       float64 `json:"adapter_mass_kg"`
	AdapterMassProvided bool    `json:"adapter_mass_provided"`
}

// CheckResult explains one rule and includes the exact source fragments used.
type CheckResult struct {
	Name               string      `json:"name"`
	Status             CheckStatus `json:"status"`
	RuleID             string      `json:"rule_id"`
	RuleVersion        string      `json:"rule_version"`
	Explanation        string      `json:"explanation"`
	EvidenceReferences []string    `json:"evidence_references"`
	Evidence           []Evidence  `json:"evidence,omitempty"`
	Relations          []Relation  `json:"relations,omitempty"`
}

// CandidateResult is the auditable machine-readable recommendation for one gripper.
type CandidateResult struct {
	CandidateID        string        `json:"candidate_id"`
	Manufacturer       string        `json:"manufacturer"`
	Model              string        `json:"model"`
	OverallStatus      OverallStatus `json:"overall_status"`
	EvidenceStrength   float64       `json:"evidence_strength"`
	EvidenceMethod     string        `json:"evidence_method"`
	Checks             []CheckResult `json:"checks"`
	MissingInformation []string      `json:"missing_information"`
	AppliedInputs      AppliedInputs `json:"applied_inputs"`
	ScopeNotes         []string      `json:"scope_notes"`
}

// CompatibilityResponse is shared by CLI JSON, MCP, and the local API.
type CompatibilityResponse struct {
	SchemaVersion string               `json:"schema_version"`
	Robot         Robot                `json:"robot"`
	Request       CompatibilityRequest `json:"request"`
	Candidates    []CandidateResult    `json:"candidates"`
}

// Evaluate validates the request and applies versioned deterministic rules to
// every gripper in the catalog. No language model participates in this method.
func Evaluate(catalog Catalog, request CompatibilityRequest) (CompatibilityResponse, error) {
	if err := catalog.Validate(); err != nil {
		return CompatibilityResponse{}, fmt.Errorf("invalid robotics catalog: %w", err)
	}
	if strings.TrimSpace(request.Robot) == "" {
		return CompatibilityResponse{}, fmt.Errorf("robot is required")
	}
	if request.WorkpieceMassKG < 0 || request.RequiredOpeningMM < 0 {
		return CompatibilityResponse{}, fmt.Errorf("workpiece mass and required opening must be non-negative")
	}
	if request.SafetyMarginKG != nil && *request.SafetyMarginKG < 0 {
		return CompatibilityResponse{}, fmt.Errorf("safety margin must be non-negative")
	}
	if request.AdapterMassKG != nil && *request.AdapterMassKG < 0 {
		return CompatibilityResponse{}, fmt.Errorf("adapter mass must be non-negative")
	}
	if request.RequiredIPRating != "" {
		if _, _, ok := parseIPRating(request.RequiredIPRating); !ok {
			return CompatibilityResponse{}, fmt.Errorf("required_ip_rating must be a concrete rating such as IP54")
		}
	}
	robot, err := catalog.FindRobot(request.Robot)
	if err != nil {
		return CompatibilityResponse{}, err
	}
	response := CompatibilityResponse{SchemaVersion: "robotics.compatibility.v1", Robot: robot, Request: request}
	for _, gripper := range catalog.Grippers {
		response.Candidates = append(response.Candidates, evaluateCandidate(catalog, robot, gripper, request))
	}
	sort.Slice(response.Candidates, func(i, j int) bool {
		return response.Candidates[i].CandidateID < response.Candidates[j].CandidateID
	})
	return response, nil
}

func evaluateCandidate(catalog Catalog, robot Robot, gripper ElectricGripper, request CompatibilityRequest) CandidateResult {
	relations := relationsBetween(catalog.Relations, gripper.ID, robot.ID)
	mechanical := mechanicalCheck(robot, gripper, relations)
	checks := []CheckResult{
		payloadCheck(robot, gripper, request, mechanical.Status),
		openingCheck(gripper, request.RequiredOpeningMM),
		mechanical,
		electricalCheck(robot, gripper),
	}
	if strings.TrimSpace(request.RequiredInterface) != "" {
		checks = append(checks, communicationCheck(robot, gripper, request.RequiredInterface))
	}
	if strings.TrimSpace(request.RequiredIPRating) != "" {
		checks = append(checks, environmentCheck(gripper, request.RequiredIPRating))
	}
	missing := make([]string, 0)
	for _, check := range checks {
		if check.Status == CheckUnknown {
			missing = append(missing, check.Name+": "+check.Explanation)
		}
	}
	margin := 0.0
	if request.SafetyMarginKG != nil {
		margin = *request.SafetyMarginKG
	}
	adapterMass := 0.0
	if request.AdapterMassKG != nil {
		adapterMass = *request.AdapterMassKG
	}
	manufacturer, model := "", ""
	if gripper.Manufacturer.Value != nil {
		manufacturer = *gripper.Manufacturer.Value
	}
	if gripper.Model.Value != nil {
		model = *gripper.Model.Value
	}
	return CandidateResult{
		CandidateID: gripper.ID, Manufacturer: manufacturer, Model: model,
		OverallStatus: overallStatus(checks), EvidenceStrength: evidenceStrength(checks),
		EvidenceMethod: "minimum confidence across evidence used by decisive non-UNKNOWN checks",
		Checks:         checks, MissingInformation: missing,
		AppliedInputs: AppliedInputs{WorkpieceMassKG: request.WorkpieceMassKG, RequiredOpeningMM: request.RequiredOpeningMM, SafetyMarginKG: margin, AdapterMassKG: adapterMass, AdapterMassProvided: request.AdapterMassKG != nil},
		ScopeNotes: []string{
			"The catalog models the 12.5 kg UR10e specification in the reviewed data sheet; verify the physical robot arm label and configured payload variant.",
			"Payload is a mass-budget check only; center of gravity, inertia, acceleration, and robot pose are not evaluated.",
			"Electrical supply checks nominal voltage only; current capacity, connector pinout, grounding, and wiring are not evaluated.",
			"Gripping force suitability is not evaluated because friction, geometry, acceleration, and grasp mode were not provided.",
			"PASS means all requested MVP checks passed; it is not a cell-level safety certification.",
		},
	}
}

func payloadCheck(robot Robot, gripper ElectricGripper, request CompatibilityRequest, mechanicalStatus CheckStatus) CheckResult {
	result := newCheckResult("payload", "robotics.payload_mass_budget", RulePayloadVersion)
	if robot.PayloadKG.Value == nil || gripper.MassKG.Value == nil || !evidenceAllowedForDecision(robot.PayloadKG.Origin) || !evidenceAllowedForDecision(gripper.MassKG.Origin) {
		result.Status = CheckUnknown
		result.Explanation = "Robot payload or gripper mass is missing, inferred-only, or lacks decision-eligible provenance."
		result.Evidence = combineEvidence(robot.PayloadKG.Evidence, gripper.MassKG.Evidence)
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		return result
	}
	if mechanicalStatus == CheckAdapterRequired && request.AdapterMassKG == nil {
		result.Status = CheckUnknown
		result.Explanation = "A documented adapter is required, but adapter_mass_kg was not provided."
		return result
	}
	margin, adapterMass := 0.0, 0.0
	if request.SafetyMarginKG != nil {
		margin = *request.SafetyMarginKG
	}
	if request.AdapterMassKG != nil {
		adapterMass = *request.AdapterMassKG
	}
	required := *gripper.MassKG.Value + request.WorkpieceMassKG + adapterMass + margin
	remaining := *robot.PayloadKG.Value - required
	result.Evidence = combineEvidence(robot.PayloadKG.Evidence, gripper.MassKG.Evidence)
	result.EvidenceReferences = evidenceIDs(result.Evidence)
	result.Explanation = fmt.Sprintf("%.3f kg payload - %.3f kg gripper - %.3f kg workpiece - %.3f kg adapter - %.3f kg explicit safety margin = %.3f kg remaining.", *robot.PayloadKG.Value, *gripper.MassKG.Value, request.WorkpieceMassKG, adapterMass, margin, remaining)
	if remaining < 0 {
		result.Status = CheckFail
	} else {
		result.Status = CheckPass
	}
	return result
}

func openingCheck(gripper ElectricGripper, required float64) CheckResult {
	result := newCheckResult("opening", "robotics.required_opening", RuleOpeningVersion)
	if gripper.OpeningMM.Value == nil {
		result.Status = CheckUnknown
		result.Explanation = gripper.OpeningMM.UnknownReason
		result.Evidence = gripper.OpeningMM.Evidence
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		if result.Explanation == "" {
			result.Explanation = "Maximum opening is not known."
		}
		return result
	}
	if gripper.OpeningMM.Semantics != "maximum_opening" || !evidenceAllowedForDecision(gripper.OpeningMM.Origin) {
		result.Status = CheckUnknown
		result.Explanation = "The available value is not a decision-eligible maximum-opening value."
		result.Evidence = gripper.OpeningMM.Evidence
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		return result
	}
	result.Evidence = gripper.OpeningMM.Evidence
	result.EvidenceReferences = evidenceIDs(result.Evidence)
	result.Explanation = fmt.Sprintf("Required opening %.3f mm compared with declared maximum opening %.3f mm.", required, *gripper.OpeningMM.Value)
	if *gripper.OpeningMM.Value < required {
		result.Status = CheckFail
	} else {
		result.Status = CheckPass
	}
	return result
}

func mechanicalCheck(robot Robot, gripper ElectricGripper, relations []Relation) CheckResult {
	result := newCheckResult("mechanical", "robotics.mechanical_interface", RuleMechanicalVersion)
	for _, relation := range relations {
		if !evidenceAllowedForDecision(relation.Origin) {
			continue
		}
		if relation.RelationType == RelationNotCompatibleWith {
			result.Status, result.Relations = CheckFail, []Relation{relation}
			result.Evidence, result.Explanation = relation.Evidence, "An evidence-backed NOT_COMPATIBLE_WITH relation applies."
			result.EvidenceReferences = evidenceIDs(result.Evidence)
			return result
		}
	}
	var inferredRelations []Relation
	for _, relation := range relations {
		if relation.Origin == OriginInferred {
			inferredRelations = append(inferredRelations, relation)
		}
	}
	for _, relation := range relations {
		if !evidenceAllowedForDecision(relation.Origin) {
			continue
		}
		switch relation.RelationType {
		case RelationMechanicallyCompatibleWith:
			result.Status, result.Relations = CheckPass, []Relation{relation}
			result.Evidence, result.Explanation = relation.Evidence, "A decision-eligible manufacturer-declared mechanical compatibility relation applies."
			result.EvidenceReferences = evidenceIDs(result.Evidence)
			return result
		case RelationRequiresAdapter:
			result.Status, result.Relations = CheckAdapterRequired, []Relation{relation}
			result.Evidence, result.Explanation = relation.Evidence, "A documented adapter is required: "+relation.Details
			result.EvidenceReferences = evidenceIDs(result.Evidence)
			return result
		}
	}
	if robot.FlangeStandard.Value != nil && gripper.MechanicalInterface.Value != nil && evidenceAllowedForDecision(robot.FlangeStandard.Origin) && evidenceAllowedForDecision(gripper.MechanicalInterface.Origin) {
		result.Evidence = combineEvidence(robot.FlangeStandard.Evidence, gripper.MechanicalInterface.Evidence)
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		if canonicalInterface(*robot.FlangeStandard.Value) == canonicalInterface(*gripper.MechanicalInterface.Value) {
			result.Status = CheckPass
			result.Explanation = "Canonical mechanical interfaces are identical."
			return result
		}
		result.Status = CheckUnknown
		result.Explanation = "Known mechanical interface strings differ, but no evidence-backed incompatibility or adapter relation exists."
		return result
	}
	result.Status = CheckUnknown
	if len(inferredRelations) > 0 {
		result.Relations = inferredRelations
		for _, relation := range inferredRelations {
			result.Evidence = combineEvidence(result.Evidence, relation.Evidence)
		}
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		result.Explanation = "Only INFERRED relation candidates are available; they are not eligible for a positive compatibility decision."
	} else {
		result.Explanation = "Robot or gripper mechanical interface is not sufficiently known."
	}
	return result
}

func electricalCheck(robot Robot, gripper ElectricGripper) CheckResult {
	result := newCheckResult("electrical", "robotics.electrical_supply", RuleElectricalVersion)
	if len(robot.SupplyVoltageV.Values) == 0 || len(gripper.SupplyVoltageV.Values) == 0 || !evidenceAllowedForDecision(robot.SupplyVoltageV.Origin) || !evidenceAllowedForDecision(gripper.SupplyVoltageV.Origin) {
		result.Status, result.Explanation = CheckUnknown, "Robot or gripper supply voltage is missing or inferred-only."
		result.Evidence = combineEvidence(robot.SupplyVoltageV.Evidence, gripper.SupplyVoltageV.Evidence)
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		return result
	}
	result.Evidence = combineEvidence(robot.SupplyVoltageV.Evidence, gripper.SupplyVoltageV.Evidence)
	result.EvidenceReferences = evidenceIDs(result.Evidence)
	for _, available := range robot.SupplyVoltageV.Values {
		for _, required := range gripper.SupplyVoltageV.Values {
			if nearlyEqual(available, required) {
				result.Status = CheckPass
				result.Explanation = fmt.Sprintf("Robot tool supply includes %.3f V required by the gripper.", required)
				return result
			}
		}
	}
	result.Status = CheckFail
	result.Explanation = fmt.Sprintf("Robot voltages %v V do not include a gripper-required voltage %v V.", robot.SupplyVoltageV.Values, gripper.SupplyVoltageV.Values)
	return result
}

func communicationCheck(robot Robot, gripper ElectricGripper, required string) CheckResult {
	result := newCheckResult("communication", "robotics.communication_interface", RuleCommunicationVersion)
	if len(robot.SupportedInterfaces.Values) == 0 || len(gripper.CommunicationInterfaces.Values) == 0 || !evidenceAllowedForDecision(robot.SupportedInterfaces.Origin) || !evidenceAllowedForDecision(gripper.CommunicationInterfaces.Origin) {
		result.Status, result.Explanation = CheckUnknown, "Robot or gripper communication interfaces are missing or inferred-only."
		result.Evidence = combineEvidence(robot.SupportedInterfaces.Evidence, gripper.CommunicationInterfaces.Evidence)
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		return result
	}
	wanted := canonicalInterface(required)
	robotHas, gripperHas := false, false
	for _, item := range robot.SupportedInterfaces.Values {
		if canonicalInterface(item) == wanted {
			robotHas = true
		}
	}
	for _, item := range gripper.CommunicationInterfaces.Values {
		if canonicalInterface(item) == wanted {
			gripperHas = true
		}
	}
	result.Evidence = combineEvidence(robot.SupportedInterfaces.Evidence, gripper.CommunicationInterfaces.Evidence)
	result.EvidenceReferences = evidenceIDs(result.Evidence)
	result.Explanation = fmt.Sprintf("Required canonical interface %s: robot=%t, gripper=%t.", wanted, robotHas, gripperHas)
	if robotHas && gripperHas {
		result.Status = CheckPass
	} else {
		result.Status = CheckFail
	}
	return result
}

func environmentCheck(gripper ElectricGripper, required string) CheckResult {
	result := newCheckResult("environment", "robotics.environment_ip", RuleEnvironmentVersion)
	if gripper.IPRating.Value == nil || !evidenceAllowedForDecision(gripper.IPRating.Origin) {
		result.Status = CheckUnknown
		result.Explanation = gripper.IPRating.UnknownReason
		result.Evidence = gripper.IPRating.Evidence
		result.EvidenceReferences = evidenceIDs(result.Evidence)
		if result.Explanation == "" {
			result.Explanation = "Gripper IP rating is missing or inferred-only."
		}
		return result
	}
	actualFirst, actualSecond, actualOK := parseIPRating(*gripper.IPRating.Value)
	requiredFirst, requiredSecond, requiredOK := parseIPRating(required)
	if !actualOK || !requiredOK {
		result.Status, result.Explanation = CheckUnknown, "IP ratings are not concrete two-digit ratings and cannot be compared."
		return result
	}
	result.Evidence = gripper.IPRating.Evidence
	result.EvidenceReferences = evidenceIDs(result.Evidence)
	result.Explanation = fmt.Sprintf("Declared %s compared digit-by-digit with required %s.", *gripper.IPRating.Value, strings.ToUpper(required))
	if actualFirst >= requiredFirst && actualSecond >= requiredSecond {
		result.Status = CheckPass
	} else {
		result.Status = CheckFail
	}
	return result
}

func newCheckResult(name, ruleID, version string) CheckResult {
	return CheckResult{Name: name, RuleID: ruleID, RuleVersion: version, EvidenceReferences: []string{}}
}

func relationsBetween(relations []Relation, first, second string) []Relation {
	var result []Relation
	for _, relation := range relations {
		if (relation.Subject == first && relation.Object == second) || (relation.Subject == second && relation.Object == first) {
			result = append(result, relation)
		}
	}
	return result
}

func overallStatus(checks []CheckResult) OverallStatus {
	for _, check := range checks {
		if check.Status == CheckFail {
			return OverallFail
		}
	}
	for _, check := range checks {
		if check.Status == CheckUnknown {
			return OverallUnknown
		}
	}
	for _, check := range checks {
		if check.Status == CheckAdapterRequired {
			return OverallConditional
		}
	}
	return OverallPass
}

func evidenceStrength(checks []CheckResult) float64 {
	strength, found := 1.0, false
	for _, check := range checks {
		if check.Status == CheckUnknown {
			continue
		}
		for _, evidence := range check.Evidence {
			found = true
			if evidence.Confidence < strength {
				strength = evidence.Confidence
			}
		}
	}
	if !found {
		return 0
	}
	return strength
}

func combineEvidence(groups ...[]Evidence) []Evidence {
	seen := make(map[string]bool)
	var result []Evidence
	for _, group := range groups {
		for _, item := range group {
			if !seen[item.ID] {
				seen[item.ID] = true
				result = append(result, item)
			}
		}
	}
	return result
}

func evidenceIDs(evidence []Evidence) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}
