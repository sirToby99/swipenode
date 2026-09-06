package knowledgecmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/packquality"
	"github.com/spf13/cobra"
)

const qualityAssessmentSchema = "swipenode.pack-quality-assessment.v1"

type qualityAssessmentDocument struct {
	SchemaVersion string                            `json:"schema_version"`
	Packs         map[string]packquality.Assessment `json:"packs"`
}

func newQualityCommand(deps Dependencies) *cobra.Command {
	var assessmentPath string
	var asJSON bool
	cmd := &cobra.Command{Use: "quality [pack-id]", Short: "Evaluate explicit managed Knowledge Pack quality gates", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root, registry, err := rootAndRegistry(cmd.Context(), deps)
		if err != nil {
			return err
		}
		assessments, err := readQualityAssessments(assessmentPath)
		if err != nil {
			return err
		}
		packs := registry.List()
		if len(args) == 1 {
			pack, ok := registry.Find(args[0])
			if !ok {
				return fmt.Errorf("unknown knowledge pack %q", args[0])
			}
			packs = []knowledge.Pack{pack}
		}
		reports := make([]packquality.Report, 0, len(packs))
		for _, pack := range packs {
			assessment := assessments[pack.ID]
			if pack.Origin == knowledge.OriginManaged {
				stateDir, err := deps.StateDir(cmd.Context(), root)
				if err != nil {
					return err
				}
				release, ok, err := distribution.OpenStore(stateDir).ActiveInstalledRelease(pack.ID)
				if err != nil {
					return fmt.Errorf("verify active managed release for %s: %w", pack.ID, err)
				}
				if ok {
					signingMode := assessment.Release.SigningMode
					assessment.Release = packquality.Release{Version: release.Version, Publisher: release.Publisher, PublisherKeyID: release.KeyID, PackageSHA256: release.PackageSHA256, SigningMode: signingMode, SignatureVerified: true}
				}
			}
			report, err := packquality.Evaluate(pack, assessment, time.Now())
			if err != nil {
				return fmt.Errorf("evaluate quality for %s: %w", pack.ID, err)
			}
			reports = append(reports, report)
		}
		sort.Slice(reports, func(i, j int) bool { return reports[i].PackID < reports[j].PackID })
		if asJSON {
			return writeJSON(cmd, struct {
				SchemaVersion string               `json:"schema_version"`
				Reports       []packquality.Report `json:"reports"`
			}{"swipenode.pack-quality-reports.v1", reports})
		}
		for _, report := range reports {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\trelease_ready=%t\n", report.PackID, report.Overall, report.ReleaseReady); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().StringVar(&assessmentPath, "assessment", "", "operator-observed quality assessment JSON")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func readQualityAssessments(path string) (map[string]packquality.Assessment, error) {
	if path == "" {
		return map[string]packquality.Assessment{}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, fmt.Errorf("unsafe quality assessment file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document qualityAssessmentDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse quality assessment: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("quality assessment has trailing content")
	}
	if document.SchemaVersion != qualityAssessmentSchema || document.Packs == nil {
		return nil, fmt.Errorf("invalid quality assessment schema")
	}
	return document.Packs, nil
}
