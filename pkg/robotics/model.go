// Package robotics provides a domain layer for evidence-backed, deterministic
// robot and electric-gripper compatibility checks.
package robotics

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ProductType identifies the two product categories supported by the MVP.
type ProductType string

const (
	ProductRobot           ProductType = "ROBOT"
	ProductElectricGripper ProductType = "ELECTRIC_GRIPPER"
)

// SourceType classifies a public source without inventing document metadata.
type SourceType string

const (
	SourceManufacturerWeb SourceType = "manufacturer_web"
	SourceManufacturerPDF SourceType = "manufacturer_pdf"
	SourceManual          SourceType = "manual"
	SourceCatalog         SourceType = "catalog"
)

// Origin records how a property or relation entered the domain model.
type Origin string

const (
	OriginDeclared Origin = "DECLARED"
	OriginDerived  Origin = "DERIVED"
	OriginInferred Origin = "INFERRED"
	OriginManual   Origin = "MANUAL"
)

// Evidence is the immutable source fragment supporting a property or relation.
// Page and revision are optional because sources do not always expose them.
type Evidence struct {
	ID               string     `json:"id"`
	SourceType       SourceType `json:"source_type"`
	SourceURL        string     `json:"source_url"`
	DocumentTitle    string     `json:"document_title,omitempty"`
	DocumentRevision string     `json:"document_revision,omitempty"`
	Page             *int       `json:"page,omitempty"`
	OriginalText     string     `json:"original_text"`
	RetrievedAt      string     `json:"retrieved_at"`
	Confidence       float64    `json:"confidence"`
}

// NumberProperty holds a normalized numeric value and its exact semantics.
type NumberProperty struct {
	Value              *float64   `json:"value,omitempty"`
	Unit               string     `json:"unit,omitempty"`
	NormalizedProperty string     `json:"normalized_property"`
	Semantics          string     `json:"semantics,omitempty"`
	Origin             Origin     `json:"origin,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
	UnknownReason      string     `json:"unknown_reason,omitempty"`
}

// NumberRangeProperty represents a declared minimum/maximum range.
type NumberRangeProperty struct {
	Min                *float64   `json:"min,omitempty"`
	Max                *float64   `json:"max,omitempty"`
	Unit               string     `json:"unit,omitempty"`
	NormalizedProperty string     `json:"normalized_property"`
	Semantics          string     `json:"semantics,omitempty"`
	Origin             Origin     `json:"origin,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
	UnknownReason      string     `json:"unknown_reason,omitempty"`
}

// NumberListProperty represents a finite set, such as supported supply voltages.
type NumberListProperty struct {
	Values             []float64  `json:"values,omitempty"`
	Unit               string     `json:"unit,omitempty"`
	NormalizedProperty string     `json:"normalized_property"`
	Semantics          string     `json:"semantics,omitempty"`
	Origin             Origin     `json:"origin,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
	UnknownReason      string     `json:"unknown_reason,omitempty"`
}

// TextProperty holds one normalized textual value.
type TextProperty struct {
	Value              *string    `json:"value,omitempty"`
	NormalizedProperty string     `json:"normalized_property"`
	Semantics          string     `json:"semantics,omitempty"`
	Origin             Origin     `json:"origin,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
	UnknownReason      string     `json:"unknown_reason,omitempty"`
}

// TextListProperty holds a normalized list of exact, non-interchangeable values.
type TextListProperty struct {
	Values             []string   `json:"values,omitempty"`
	NormalizedProperty string     `json:"normalized_property"`
	Semantics          string     `json:"semantics,omitempty"`
	Origin             Origin     `json:"origin,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
	UnknownReason      string     `json:"unknown_reason,omitempty"`
}

// Robot is the canonical MVP schema for an industrial robot arm.
type Robot struct {
	ID                     string             `json:"id"`
	ProductType            ProductType        `json:"product_type"`
	Manufacturer           TextProperty       `json:"manufacturer"`
	Model                  TextProperty       `json:"model"`
	ManufacturerPartNumber TextProperty       `json:"manufacturer_part_number"`
	PayloadKG              NumberProperty     `json:"payload_kg"`
	ReachMM                NumberProperty     `json:"reach_mm"`
	MassKG                 NumberProperty     `json:"mass_kg"`
	FlangeStandard         TextProperty       `json:"flange_standard"`
	SupplyVoltageV         NumberListProperty `json:"supply_voltage_v"`
	SupportedInterfaces    TextListProperty   `json:"supported_interfaces"`
}

// ElectricGripper is the canonical MVP schema for an electric gripper.
// OpeningMM is deliberately separate from StrokeMM: the engine never assumes
// that per-jaw travel, total stroke, opening, and gripping range are synonyms.
type ElectricGripper struct {
	ID                      string              `json:"id"`
	ProductType             ProductType         `json:"product_type"`
	Manufacturer            TextProperty        `json:"manufacturer"`
	Model                   TextProperty        `json:"model"`
	ManufacturerPartNumber  TextProperty        `json:"manufacturer_part_number"`
	MassKG                  NumberProperty      `json:"mass_kg"`
	StrokeMM                NumberProperty      `json:"stroke_mm"`
	OpeningMM               NumberProperty      `json:"opening_mm"`
	GrippingForceN          NumberRangeProperty `json:"gripping_force_n"`
	MaxFingerLengthMM       NumberProperty      `json:"max_finger_length_mm"`
	SupplyVoltageV          NumberListProperty  `json:"supply_voltage_v"`
	ElectricalInterface     TextProperty        `json:"electrical_interface"`
	CommunicationInterfaces TextListProperty    `json:"communication_interfaces"`
	MechanicalInterface     TextProperty        `json:"mechanical_interface"`
	IPRating                TextProperty        `json:"ip_rating"`
}

// RelationType describes the graph semantics supported without requiring a
// graph database.
type RelationType string

const (
	RelationCompatibleWith             RelationType = "COMPATIBLE_WITH"
	RelationRequiresAdapter            RelationType = "REQUIRES_ADAPTER"
	RelationMechanicallyCompatibleWith RelationType = "MECHANICALLY_COMPATIBLE_WITH"
	RelationElectricallyCompatibleWith RelationType = "ELECTRICALLY_COMPATIBLE_WITH"
	RelationCommunicatesWith           RelationType = "COMMUNICATES_WITH"
	RelationNotCompatibleWith          RelationType = "NOT_COMPATIBLE_WITH"
	RelationUnknownCompatibility       RelationType = "UNKNOWN_COMPATIBILITY"
)

// Relation stores an evidence-backed edge between two catalog subjects.
type Relation struct {
	Subject      string       `json:"subject"`
	RelationType RelationType `json:"relation_type"`
	Object       string       `json:"object"`
	Origin       Origin       `json:"origin"`
	Confidence   float64      `json:"confidence"`
	Evidence     []Evidence   `json:"evidence"`
	RuleID       string       `json:"rule_id,omitempty"`
	RuleVersion  string       `json:"rule_version,omitempty"`
	Details      string       `json:"details,omitempty"`
}

func evidenceAllowedForDecision(origin Origin) bool {
	return origin == OriginDeclared || origin == OriginDerived || origin == OriginManual
}

func validateEvidence(e Evidence) error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("evidence id is required")
	}
	switch e.SourceType {
	case SourceManufacturerWeb, SourceManufacturerPDF, SourceManual, SourceCatalog:
	default:
		return fmt.Errorf("evidence %s has invalid source_type %q", e.ID, e.SourceType)
	}
	parsed, err := url.ParseRequestURI(e.SourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("evidence %s has invalid public source_url", e.ID)
	}
	if strings.TrimSpace(e.OriginalText) == "" {
		return fmt.Errorf("evidence %s is missing original_text", e.ID)
	}
	if _, err := time.Parse(time.RFC3339, e.RetrievedAt); err != nil {
		return fmt.Errorf("evidence %s has invalid retrieved_at: %w", e.ID, err)
	}
	if e.Confidence <= 0 || e.Confidence > 1 {
		return fmt.Errorf("evidence %s confidence must be in (0,1]", e.ID)
	}
	if e.Page != nil && *e.Page < 1 {
		return fmt.Errorf("evidence %s page must be positive", e.ID)
	}
	return nil
}

func validateEvidenceChain(name string, known bool, origin Origin, evidence []Evidence, unknownReason string) error {
	if !known {
		if strings.TrimSpace(unknownReason) == "" {
			return fmt.Errorf("%s is unknown without unknown_reason", name)
		}
		for _, item := range evidence {
			if err := validateEvidence(item); err != nil {
				return fmt.Errorf("%s ambiguity evidence: %w", name, err)
			}
		}
		return nil
	}
	if !evidenceAllowedForDecision(origin) && origin != OriginInferred {
		return fmt.Errorf("%s has invalid origin %q", name, origin)
	}
	if len(evidence) == 0 {
		return fmt.Errorf("%s is known but has no evidence", name)
	}
	for _, item := range evidence {
		if err := validateEvidence(item); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func validateNumber(name, normalized, unit string, property NumberProperty) error {
	if property.NormalizedProperty != normalized {
		return fmt.Errorf("%s normalized_property = %q, want %q", name, property.NormalizedProperty, normalized)
	}
	if property.Value != nil && property.Unit != unit {
		return fmt.Errorf("%s unit = %q, want %q", name, property.Unit, unit)
	}
	if property.Value != nil && *property.Value < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	return validateEvidenceChain(name, property.Value != nil, property.Origin, property.Evidence, property.UnknownReason)
}

func validateText(name, normalized string, property TextProperty) error {
	if property.NormalizedProperty != normalized {
		return fmt.Errorf("%s normalized_property = %q, want %q", name, property.NormalizedProperty, normalized)
	}
	known := property.Value != nil && strings.TrimSpace(*property.Value) != ""
	return validateEvidenceChain(name, known, property.Origin, property.Evidence, property.UnknownReason)
}

func validateNumberList(name, normalized, unit string, property NumberListProperty) error {
	if property.NormalizedProperty != normalized {
		return fmt.Errorf("%s normalized_property = %q, want %q", name, property.NormalizedProperty, normalized)
	}
	if len(property.Values) > 0 && property.Unit != unit {
		return fmt.Errorf("%s unit = %q, want %q", name, property.Unit, unit)
	}
	for _, value := range property.Values {
		if value < 0 {
			return fmt.Errorf("%s values must be non-negative", name)
		}
	}
	return validateEvidenceChain(name, len(property.Values) > 0, property.Origin, property.Evidence, property.UnknownReason)
}

func validateTextList(name, normalized string, property TextListProperty) error {
	if property.NormalizedProperty != normalized {
		return fmt.Errorf("%s normalized_property = %q, want %q", name, property.NormalizedProperty, normalized)
	}
	return validateEvidenceChain(name, len(property.Values) > 0, property.Origin, property.Evidence, property.UnknownReason)
}

// Validate verifies the canonical schema and rejects every known property that
// lacks traceable source evidence.
func (r Robot) Validate() error {
	if strings.TrimSpace(r.ID) == "" || r.ProductType != ProductRobot {
		return fmt.Errorf("robot must have an id and product_type ROBOT")
	}
	checks := []func() error{
		func() error { return validateText("robot.manufacturer", "manufacturer", r.Manufacturer) },
		func() error { return validateText("robot.model", "model", r.Model) },
		func() error {
			return validateText("robot.manufacturer_part_number", "manufacturer_part_number", r.ManufacturerPartNumber)
		},
		func() error { return validateNumber("robot.payload_kg", "payload_kg", "kg", r.PayloadKG) },
		func() error { return validateNumber("robot.reach_mm", "reach_mm", "mm", r.ReachMM) },
		func() error { return validateNumber("robot.mass_kg", "mass_kg", "kg", r.MassKG) },
		func() error { return validateText("robot.flange_standard", "flange_standard", r.FlangeStandard) },
		func() error {
			return validateNumberList("robot.supply_voltage_v", "supply_voltage_v", "V", r.SupplyVoltageV)
		},
		func() error {
			return validateTextList("robot.supported_interfaces", "supported_interfaces", r.SupportedInterfaces)
		},
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("robot %s: %w", r.ID, err)
		}
	}
	return nil
}

// Validate verifies the electric-gripper schema and its evidence chains.
func (g ElectricGripper) Validate() error {
	if strings.TrimSpace(g.ID) == "" || g.ProductType != ProductElectricGripper {
		return fmt.Errorf("electric gripper must have an id and product_type ELECTRIC_GRIPPER")
	}
	checks := []func() error{
		func() error { return validateText("gripper.manufacturer", "manufacturer", g.Manufacturer) },
		func() error { return validateText("gripper.model", "model", g.Model) },
		func() error {
			return validateText("gripper.manufacturer_part_number", "manufacturer_part_number", g.ManufacturerPartNumber)
		},
		func() error { return validateNumber("gripper.mass_kg", "mass_kg", "kg", g.MassKG) },
		func() error { return validateNumber("gripper.stroke_mm", "stroke_mm", "mm", g.StrokeMM) },
		func() error { return validateNumber("gripper.opening_mm", "opening_mm", "mm", g.OpeningMM) },
		func() error {
			return validateNumber("gripper.max_finger_length_mm", "max_finger_length_mm", "mm", g.MaxFingerLengthMM)
		},
		func() error {
			return validateNumberList("gripper.supply_voltage_v", "supply_voltage_v", "V", g.SupplyVoltageV)
		},
		func() error {
			return validateText("gripper.electrical_interface", "electrical_interface", g.ElectricalInterface)
		},
		func() error {
			return validateTextList("gripper.communication_interfaces", "communication_interfaces", g.CommunicationInterfaces)
		},
		func() error {
			return validateText("gripper.mechanical_interface", "mechanical_interface", g.MechanicalInterface)
		},
		func() error { return validateText("gripper.ip_rating", "ip_rating", g.IPRating) },
	}
	if g.GrippingForceN.NormalizedProperty != "gripping_force_n" {
		return fmt.Errorf("gripper %s: gripping_force_n has invalid normalized_property", g.ID)
	}
	knownRange := g.GrippingForceN.Min != nil || g.GrippingForceN.Max != nil
	if knownRange && (g.GrippingForceN.Min == nil || g.GrippingForceN.Max == nil || g.GrippingForceN.Unit != "N") {
		return fmt.Errorf("gripper %s: gripping_force_n requires min, max, and unit N", g.ID)
	}
	if knownRange && (*g.GrippingForceN.Min < 0 || *g.GrippingForceN.Max < *g.GrippingForceN.Min) {
		return fmt.Errorf("gripper %s: gripping_force_n range is invalid", g.ID)
	}
	if err := validateEvidenceChain("gripper.gripping_force_n", knownRange, g.GrippingForceN.Origin, g.GrippingForceN.Evidence, g.GrippingForceN.UnknownReason); err != nil {
		return fmt.Errorf("gripper %s: %w", g.ID, err)
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return fmt.Errorf("gripper %s: %w", g.ID, err)
		}
	}
	return nil
}

// Validate verifies that a relation cannot silently become an unsupported fact.
func (r Relation) Validate() error {
	if strings.TrimSpace(r.Subject) == "" || strings.TrimSpace(r.Object) == "" {
		return fmt.Errorf("relation subject and object are required")
	}
	switch r.RelationType {
	case RelationCompatibleWith, RelationRequiresAdapter, RelationMechanicallyCompatibleWith,
		RelationElectricallyCompatibleWith, RelationCommunicatesWith, RelationNotCompatibleWith,
		RelationUnknownCompatibility:
	default:
		return fmt.Errorf("invalid relation_type %q", r.RelationType)
	}
	if r.Confidence <= 0 || r.Confidence > 1 {
		return fmt.Errorf("relation confidence must be in (0,1]")
	}
	return validateEvidenceChain("relation", true, r.Origin, r.Evidence, "")
}
