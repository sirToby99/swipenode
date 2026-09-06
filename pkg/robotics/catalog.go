package robotics

import (
	"fmt"
	"sort"
	"strings"
)

const catalogRetrievedAt = "2026-08-27T21:40:50Z"

// Catalog is the small, controlled MVP dataset. It intentionally uses an
// in-memory representation: graph semantics come from Relations, not a graph DB.
type Catalog struct {
	Robots    []Robot           `json:"robots"`
	Grippers  []ElectricGripper `json:"electric_grippers"`
	Relations []Relation        `json:"relations"`
}

func stringPointer(value string) *string  { return &value }
func floatPointer(value float64) *float64 { return &value }

func evidence(id string, sourceType SourceType, sourceURL, title, revision, original string, confidence float64) Evidence {
	return Evidence{
		ID: id, SourceType: sourceType, SourceURL: sourceURL, DocumentTitle: title,
		DocumentRevision: revision, OriginalText: original, RetrievedAt: catalogRetrievedAt,
		Confidence: confidence,
	}
}

func textProperty(value, name, semantics string, origin Origin, evidence ...Evidence) TextProperty {
	return TextProperty{Value: stringPointer(value), NormalizedProperty: name, Semantics: semantics, Origin: origin, Evidence: evidence}
}

func unknownText(name, reason string) TextProperty {
	return TextProperty{NormalizedProperty: name, UnknownReason: reason}
}

func numberProperty(value float64, unit, name, semantics string, origin Origin, evidence ...Evidence) NumberProperty {
	return NumberProperty{Value: floatPointer(value), Unit: unit, NormalizedProperty: name, Semantics: semantics, Origin: origin, Evidence: evidence}
}

func unknownNumber(name, reason string) NumberProperty {
	return NumberProperty{NormalizedProperty: name, UnknownReason: reason}
}

func numberListProperty(values []float64, unit, name, semantics string, origin Origin, evidence ...Evidence) NumberListProperty {
	return NumberListProperty{Values: values, Unit: unit, NormalizedProperty: name, Semantics: semantics, Origin: origin, Evidence: evidence}
}

func textListProperty(values []string, name, semantics string, origin Origin, evidence ...Evidence) TextListProperty {
	return TextListProperty{Values: values, NormalizedProperty: name, Semantics: semantics, Origin: origin, Evidence: evidence}
}

func rangeProperty(minimum, maximum float64, unit, name, semantics string, origin Origin, evidence ...Evidence) NumberRangeProperty {
	return NumberRangeProperty{Min: floatPointer(minimum), Max: floatPointer(maximum), Unit: unit, NormalizedProperty: name, Semantics: semantics, Origin: origin, Evidence: evidence}
}

func robotiqGripper(model, id, partNumber string, mass, opening, forceMin, forceMax float64, productURL string) ElectricGripper {
	evidencePrefix := strings.ReplaceAll(id, ":", "-")
	tableModel := strings.Replace(model, "2F-", "2-FINGER ", 1)
	massGrams := fmt.Sprintf("%.0f", mass*1000)
	if nearlyEqual(mass, 1.025) {
		massGrams = "1,025"
	}
	manualURL := "https://blog.robotiq.com/hubfs/support-files/2F-85_2F-140_UR_PDF_2026-03-17.pdf"
	openingURL := "https://assets.robotiq.com/website-assets/support_documents/document/online/2F-85_2F-140_TM_InstructionManual_HTML5_20190206.zip/2F-85_2F-140_TM_InstructionManual_HTML5/Content/1.%20General_Presentation.htm"
	installURL := "https://assets.robotiq.com/website-assets/support_documents/document/online/2F-85_2F-140_TM_InstructionManual_HTML5_20190503.zip/2F-85_2F-140_TM_InstructionManual_HTML5/Content/3.%20Installation.htm"
	partsURL := "https://assets.robotiq.com/website-assets/support_documents/document/online/2F-85_2F-140_Instruction_Manual_Gen_HTML_20190524.zip/2F-85_2F-140_Instruction_Manual_Gen_HTML/Content/8.%20Spare_Parts_Kits_Accessories.htm"
	specURL := "https://assets.robotiq.com/website-assets/support_documents/document/online/2F-85_2F-140_TM_InstructionManual_HTML5_20190315.zip/2F-85_2F-140_TM_InstructionManual_HTML5/Content/6.%20Specifications.htm"
	modelEvidence := evidence(evidencePrefix+"-identity", SourceManufacturerWeb, productURL, model, "", model+"; Manufacturer: Robotiq", 0.99)
	partEvidence := evidence(evidencePrefix+"-part-number", SourceManual, partsURL, "2F-85 & 2F-140 Instruction Manual — Spare Parts", "", "Gripper basic unit: "+partNumber, 0.99)
	massEvidence := evidence(evidencePrefix+"-mass", SourceManual, manualURL, "Robotiq 2F-85 & 2F-140 Instruction Manual", "", fmt.Sprintf("Weight | %s %s g. All specs are measured with coupling GRP-CPL-062 and the documented fingertip.", tableModel, massGrams), 0.99)
	openingEvidence := evidence(evidencePrefix+"-opening", SourceManual, openingURL, "Robotiq 2F-85 & 2F-140 Instruction Manual", "", "The 2-Finger version will change finger opening dimensions, which will be 85 mm (2F-85) or 140 mm (2F-140).", 0.99)
	forceEvidence := evidence(evidencePrefix+"-force", SourceManual, manualURL, "Robotiq 2F-85 & 2F-140 Instruction Manual", "", fmt.Sprintf("Grasp Force | %s %.0f to %.0f N", tableModel, forceMin, forceMax), 0.99)
	powerEvidence := evidence("robotiq-2f-power", SourceManual, installURL, "Robotiq 2F-85 & 2F-140 Instruction Manual — Installation", "", "Output voltage: 24 V DC ±10%; serial RS485 communication", 0.99)
	mechanicalEvidence := evidence("robotiq-2f-coupling", SourceManual, specURL, "Robotiq 2F-85 & 2F-140 Instruction Manual — Specifications", "", "Coupling GRP-CPL-062: ISO 9409-1 standard 50-4-M6", 0.99)
	ipEvidence := evidence("robotiq-2f-ip", SourceManual, installURL, "Robotiq 2F-85 & 2F-140 Instruction Manual — Installation", "", "IP Rating: IP 40", 0.99)
	return ElectricGripper{
		ID: id, ProductType: ProductElectricGripper,
		Manufacturer:            textProperty("Robotiq", "manufacturer", "manufacturer_name", OriginDeclared, modelEvidence),
		Model:                   textProperty(model, "model", "manufacturer_model", OriginDeclared, modelEvidence),
		ManufacturerPartNumber:  textProperty(partNumber, "manufacturer_part_number", "basic_gripper_unit", OriginDeclared, partEvidence),
		MassKG:                  numberProperty(mass, "kg", "mass_kg", "gripper_fitted_with_documented_coupling", OriginDeclared, massEvidence),
		StrokeMM:                numberProperty(opening, "mm", "stroke_mm", "declared_total_programmable_stroke", OriginDeclared, openingEvidence),
		OpeningMM:               numberProperty(opening, "mm", "opening_mm", "maximum_opening", OriginDeclared, openingEvidence),
		GrippingForceN:          rangeProperty(forceMin, forceMax, "N", "gripping_force_n", "programmable_grasp_force_range", OriginDeclared, forceEvidence),
		MaxFingerLengthMM:       unknownNumber("max_finger_length_mm", "The reviewed manufacturer sources do not declare a maximum finger length for this configuration."),
		SupplyVoltageV:          numberListProperty([]float64{24}, "V", "supply_voltage_v", "required_dc_supply", OriginDeclared, powerEvidence),
		ElectricalInterface:     textProperty("24 V DC power input", "electrical_interface", "device_cable_power", OriginDeclared, powerEvidence),
		CommunicationInterfaces: textListProperty([]string{"MODBUS RTU", "RS-485"}, "communication_interfaces", "native_protocol_and_physical_layer", OriginDeclared, powerEvidence),
		MechanicalInterface:     textProperty("ISO 9409-1-50-4-M6", "mechanical_interface", "documented_coupling_to_wrist_bolt_pattern", OriginDeclared, mechanicalEvidence),
		IPRating:                textProperty("IP40", "ip_rating", "declared_product_rating", OriginDeclared, ipEvidence),
	}
}

// DefaultCatalog returns the reviewed proof-of-value catalog: one UR10e and
// three real electric grippers from Robotiq and SCHUNK.
func DefaultCatalog() (Catalog, error) {
	urURL := "https://www.universal-robots.com/manuals/EN/DataSheets/UR10e_techsheet_pdf_online/UR10e_techsheet_en.pdf"
	urIdentity := evidence("ur10e-identity", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Universal Robots A/S — UR10e", 0.99)
	urPayload := evidence("ur10e-payload", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Payload: 12.5 kg", 0.99)
	urReach := evidence("ur10e-reach", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Reach: 1300 mm", 0.99)
	urMass := evidence("ur10e-mass", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Robot arm weight including cable: 33.3 kg", 0.99)
	urFlange := evidence("ur10e-flange", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Tool flange: EN ISO-9409-1-50-4-M6", 0.99)
	urSupply := evidence("ur10e-tool-supply", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Tool I/O power supply voltage: 12/24 V", 0.99)
	urInterfaces := evidence("ur10e-interfaces", SourceManufacturerPDF, urURL, "UR10e Technical Specification", "Updated May 2025", "Modbus TCP; Ethernet/IP; PROFINET; tool interface RS485; digital I/O", 0.99)

	robot := Robot{
		ID: "universal-robots:ur10e", ProductType: ProductRobot,
		Manufacturer:           textProperty("Universal Robots", "manufacturer", "manufacturer_name", OriginDeclared, urIdentity),
		Model:                  textProperty("UR10e", "model", "manufacturer_model", OriginDeclared, urIdentity),
		ManufacturerPartNumber: unknownText("manufacturer_part_number", "No manufacturer part number was present in the reviewed UR10e technical specification."),
		PayloadKG:              numberProperty(12.5, "kg", "payload_kg", "maximum_declared_payload_mass", OriginDeclared, urPayload),
		ReachMM:                numberProperty(1300, "mm", "reach_mm", "maximum_reach", OriginDeclared, urReach),
		MassKG:                 numberProperty(33.3, "kg", "mass_kg", "robot_arm_including_cable", OriginDeclared, urMass),
		FlangeStandard:         textProperty("ISO 9409-1-50-4-M6", "flange_standard", "tool_flange", OriginDeclared, urFlange),
		SupplyVoltageV:         numberListProperty([]float64{12, 24}, "V", "supply_voltage_v", "tool_io_available_voltage", OriginDeclared, urSupply),
		SupportedInterfaces:    textListProperty([]string{"MODBUS TCP", "ETHERNET/IP", "PROFINET", "RS-485", "DIGITAL I/O"}, "supported_interfaces", "controller_and_tool_interfaces", OriginDeclared, urInterfaces),
	}

	twoF85URL := "https://www.universal-robots.com/marketplace/products/01tP40000071NgHIAU/"
	twoF140URL := "https://www.universal-robots.com/marketplace/products/01tP40000071NgIIAU/"
	twoF85 := robotiqGripper("2F-85", "robotiq:2f-85", "AGC-GRP-2F85", 0.925, 85, 20, 235, twoF85URL)
	twoF140 := robotiqGripper("2F-140", "robotiq:2f-140", "AGC-GRP-2F140", 1.025, 140, 10, 125, twoF140URL)

	schunkURL := "https://schunk.com/in/en/gripping-systems/parallel-gripper/egu/egu-80-pn-m-b/p/000000000001491586"
	schunkIdentity := evidence("schunk-egu80-identity", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "EGU 80-PN-M-B; ID 1491586", 0.99)
	schunkMass := evidence("schunk-egu80-mass", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Weight: 7.72 kg", 0.99)
	schunkStroke := evidence("schunk-egu80-stroke", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Stroke per jaw: 80.0 mm", 0.99)
	schunkForce := evidence("schunk-egu80-force", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Minimum gripping force 1000 N; maximum 4000 N", 0.99)
	schunkFinger := evidence("schunk-egu80-finger-length", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Maximum permissible finger length: 200.0 mm", 0.99)
	schunkPower := evidence("schunk-egu80-power", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Nominal voltage: 24.0 V", 0.99)
	schunkComms := evidence("schunk-egu80-communication", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "Communication interface: PROFINET", 0.99)
	schunkIP := evidence("schunk-egu80-ip-components", SourceManufacturerWeb, schunkURL, "SCHUNK EGU 80-PN-M-B", "", "IP protection class, electronics: 67; guide/base jaws: 40", 0.99)
	schunk := ElectricGripper{
		ID: "schunk:egu-80-pn-m-b", ProductType: ProductElectricGripper,
		Manufacturer:            textProperty("SCHUNK", "manufacturer", "manufacturer_name", OriginDeclared, schunkIdentity),
		Model:                   textProperty("EGU 80-PN-M-B", "model", "manufacturer_model", OriginDeclared, schunkIdentity),
		ManufacturerPartNumber:  textProperty("1491586", "manufacturer_part_number", "manufacturer_product_id", OriginDeclared, schunkIdentity),
		MassKG:                  numberProperty(7.72, "kg", "mass_kg", "product_weight", OriginDeclared, schunkMass),
		StrokeMM:                numberProperty(80, "mm", "stroke_mm", "stroke_per_jaw", OriginDeclared, schunkStroke),
		OpeningMM:               NumberProperty{NormalizedProperty: "opening_mm", UnknownReason: "Manufacturer declares stroke per jaw, not an absolute maximum opening or gripping range; semantics are not interchangeable.", Evidence: []Evidence{schunkStroke}},
		GrippingForceN:          rangeProperty(1000, 4000, "N", "gripping_force_n", "declared_gripping_force_range", OriginDeclared, schunkForce),
		MaxFingerLengthMM:       numberProperty(200, "mm", "max_finger_length_mm", "maximum_permissible_finger_length", OriginDeclared, schunkFinger),
		SupplyVoltageV:          numberListProperty([]float64{24}, "V", "supply_voltage_v", "nominal_supply", OriginDeclared, schunkPower),
		ElectricalInterface:     textProperty("24 V power input", "electrical_interface", "nominal_supply_input", OriginDeclared, schunkPower),
		CommunicationInterfaces: textListProperty([]string{"PROFINET"}, "communication_interfaces", "native_protocol", OriginDeclared, schunkComms),
		MechanicalInterface:     unknownText("mechanical_interface", "The reviewed manufacturer product page does not declare a normalized robot-side mechanical interface."),
		IPRating:                TextProperty{NormalizedProperty: "ip_rating", UnknownReason: "Manufacturer gives IP67 for electronics and IP40 for guide/base jaws; no single whole-gripper IP rating is asserted.", Evidence: []Evidence{schunkIP}},
	}

	compatibilityEvidence85 := evidence("ur-marketplace-2f85-ur10e", SourceCatalog, twoF85URL, "Universal Robots Marketplace — Robotiq 2F-85", "", "e-Series ready, connecting directly to the wrist; compatibility includes UR10e", 0.98)
	compatibilityEvidence140 := evidence("ur-marketplace-2f140-ur10e", SourceCatalog, twoF140URL, "Universal Robots Marketplace — Robotiq 2F-140", "", "e-Series ready, connecting directly to the wrist; compatibility includes UR10e", 0.98)
	catalog := Catalog{
		Robots:   []Robot{robot},
		Grippers: []ElectricGripper{twoF85, twoF140, schunk},
		Relations: []Relation{
			{Subject: twoF85.ID, RelationType: RelationMechanicallyCompatibleWith, Object: robot.ID, Origin: OriginDeclared, Confidence: 0.98, Evidence: []Evidence{compatibilityEvidence85}, Details: "Reviewed configured product includes the documented coupling."},
			{Subject: twoF140.ID, RelationType: RelationMechanicallyCompatibleWith, Object: robot.ID, Origin: OriginDeclared, Confidence: 0.98, Evidence: []Evidence{compatibilityEvidence140}, Details: "Reviewed configured product includes the documented coupling."},
		},
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Validate ensures ids are unique and all known facts carry valid evidence.
func (c Catalog) Validate() error {
	ids := make(map[string]struct{})
	for _, robot := range c.Robots {
		if _, exists := ids[robot.ID]; exists {
			return fmt.Errorf("duplicate catalog id %s", robot.ID)
		}
		ids[robot.ID] = struct{}{}
		if err := robot.Validate(); err != nil {
			return err
		}
	}
	for _, gripper := range c.Grippers {
		if _, exists := ids[gripper.ID]; exists {
			return fmt.Errorf("duplicate catalog id %s", gripper.ID)
		}
		ids[gripper.ID] = struct{}{}
		if err := gripper.Validate(); err != nil {
			return err
		}
	}
	for _, relation := range c.Relations {
		if _, ok := ids[relation.Subject]; !ok {
			return fmt.Errorf("relation subject %s is not in catalog", relation.Subject)
		}
		if _, ok := ids[relation.Object]; !ok {
			return fmt.Errorf("relation object %s is not in catalog", relation.Object)
		}
		if err := relation.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// FindRobot performs a case-insensitive exact match on id, model, or
// "manufacturer model". It deliberately avoids fuzzy guessing.
func (c Catalog) FindRobot(query string) (Robot, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, robot := range c.Robots {
		manufacturer, model := "", ""
		if robot.Manufacturer.Value != nil {
			manufacturer = *robot.Manufacturer.Value
		}
		if robot.Model.Value != nil {
			model = *robot.Model.Value
		}
		aliases := []string{robot.ID, model, manufacturer + " " + model}
		for _, alias := range aliases {
			if strings.ToLower(strings.TrimSpace(alias)) == query {
				return robot, nil
			}
		}
	}
	return Robot{}, fmt.Errorf("robot %q not found; available: %s", query, strings.Join(c.RobotNames(), ", "))
}

// RobotNames returns stable display names for error messages and discovery.
func (c Catalog) RobotNames() []string {
	names := make([]string, 0, len(c.Robots))
	for _, robot := range c.Robots {
		if robot.Model.Value != nil {
			names = append(names, *robot.Model.Value)
		}
	}
	sort.Strings(names)
	return names
}
