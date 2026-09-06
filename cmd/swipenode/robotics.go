package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/sirToby99/swipenode/pkg/robotics"
	"github.com/spf13/cobra"
)

type roboticsCLIOptions struct {
	Robot             string
	WorkpieceMassKG   float64
	RequiredOpeningMM float64
	RequiredIPRating  string
	RequiredInterface string
	SafetyMarginKG    float64
	SafetyMarginSet   bool
	AdapterMassKG     float64
	AdapterMassSet    bool
	JSON              bool
}

var roboticsOptions roboticsCLIOptions

var roboticsCmd = &cobra.Command{
	Use:   "robotics",
	Short: "Evidence-backed deterministic robotics compatibility checks",
}

var roboticsGrippersCmd = &cobra.Command{
	Use:   "grippers",
	Short: "Evaluate electric grippers for a robot and workpiece",
	RunE: func(cmd *cobra.Command, args []string) error {
		roboticsOptions.SafetyMarginSet = cmd.Flags().Changed("safety-margin")
		roboticsOptions.AdapterMassSet = cmd.Flags().Changed("adapter-mass")
		return runRoboticsGrippers(cmd.OutOrStdout(), roboticsOptions)
	},
}

func runRoboticsGrippers(output io.Writer, options roboticsCLIOptions) error {
	catalog, err := robotics.DefaultCatalog()
	if err != nil {
		return fmt.Errorf("load robotics catalog: %w", err)
	}
	request := robotics.CompatibilityRequest{
		Robot: options.Robot, WorkpieceMassKG: options.WorkpieceMassKG,
		RequiredOpeningMM: options.RequiredOpeningMM,
		RequiredIPRating:  options.RequiredIPRating, RequiredInterface: options.RequiredInterface,
	}
	if options.SafetyMarginSet {
		request.SafetyMarginKG = &options.SafetyMarginKG
	}
	if options.AdapterMassSet {
		request.AdapterMassKG = &options.AdapterMassKG
	}
	response, err := robotics.Evaluate(catalog, request)
	if err != nil {
		return fmt.Errorf("robotics compatibility: %w", err)
	}
	if options.JSON {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("encode robotics result: %w", err)
		}
		return nil
	}
	writeRoboticsHuman(output, response)
	return nil
}

func writeRoboticsHuman(output io.Writer, response robotics.CompatibilityResponse) {
	robotModel := response.Request.Robot
	if response.Robot.Model.Value != nil {
		robotModel = *response.Robot.Model.Value
	}
	fmt.Fprintf(output, "Robot: %s\n", robotModel)
	fmt.Fprintf(output, "Workpiece: %.3f kg; required opening: %.3f mm\n\n", response.Request.WorkpieceMassKG, response.Request.RequiredOpeningMM)
	for _, candidate := range response.Candidates {
		fmt.Fprintf(output, "%s %s\n", candidate.Manufacturer, candidate.Model)
		fmt.Fprintf(output, "Overall: %s\n", candidate.OverallStatus)
		fmt.Fprintf(output, "Evidence strength: %.2f (%s)\n", candidate.EvidenceStrength, candidate.EvidenceMethod)
		fmt.Fprintln(output, "Checks:")
		for _, check := range candidate.Checks {
			fmt.Fprintf(output, "  %s: %s\n", check.Name, check.Status)
			fmt.Fprintf(output, "    %s\n", check.Explanation)
			for _, item := range check.Evidence {
				title := strings.TrimSpace(item.DocumentTitle)
				if title != "" {
					fmt.Fprintf(output, "    Evidence %s: %s — %s\n", item.ID, title, item.SourceURL)
				} else {
					fmt.Fprintf(output, "    Evidence %s: %s\n", item.ID, item.SourceURL)
				}
			}
		}
		if len(candidate.MissingInformation) > 0 {
			fmt.Fprintln(output, "Missing information:")
			for _, missing := range candidate.MissingInformation {
				fmt.Fprintf(output, "  - %s\n", missing)
			}
		}
		fmt.Fprintln(output)
	}
}

func init() {
	roboticsGrippersCmd.Flags().StringVar(&roboticsOptions.Robot, "robot", "", "robot model or canonical id")
	roboticsGrippersCmd.Flags().Float64Var(&roboticsOptions.WorkpieceMassKG, "workpiece-mass", 0, "workpiece mass in kg")
	roboticsGrippersCmd.Flags().Float64Var(&roboticsOptions.RequiredOpeningMM, "required-opening", 0, "required maximum opening in mm")
	roboticsGrippersCmd.Flags().StringVar(&roboticsOptions.RequiredIPRating, "required-ip-rating", "", "optional concrete IP requirement, for example IP54")
	roboticsGrippersCmd.Flags().StringVar(&roboticsOptions.RequiredInterface, "required-interface", "", "optional required communication interface")
	roboticsGrippersCmd.Flags().Float64Var(&roboticsOptions.SafetyMarginKG, "safety-margin", 0, "explicit payload safety margin in kg")
	roboticsGrippersCmd.Flags().Float64Var(&roboticsOptions.AdapterMassKG, "adapter-mass", 0, "adapter mass in kg when an adapter is required")
	roboticsGrippersCmd.Flags().BoolVar(&roboticsOptions.JSON, "json", false, "emit machine-readable JSON")
	_ = roboticsGrippersCmd.MarkFlagRequired("robot")
	_ = roboticsGrippersCmd.MarkFlagRequired("workpiece-mass")
	_ = roboticsGrippersCmd.MarkFlagRequired("required-opening")
	roboticsCmd.AddCommand(roboticsGrippersCmd)
	rootCmd.AddCommand(roboticsCmd)
}
