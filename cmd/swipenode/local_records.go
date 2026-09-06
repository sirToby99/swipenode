package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sirToby99/swipenode/internal/controlplane"
	"github.com/spf13/cobra"
)

func newEvidenceCommand() *cobra.Command {
	command := &cobra.Command{Use: "evidence", Short: "Inspect local versioned Evidence records", Args: cobra.NoArgs}
	command.AddCommand(newEvidenceListCommand(), newEvidenceShowCommand())
	return command
}

func newEvidenceListCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		service, err := openControlPlaneService(command)
		if err != nil {
			return err
		}
		listing, err := service.Evidence()
		if err != nil {
			return err
		}
		if asJSON {
			return writeLocalRecordJSON(command, listing)
		}
		for _, record := range listing.Evidence {
			if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", record.ID, record.PackID, record.SourceID, record.DocumentRevision, record.ContentSHA256); err != nil {
				return err
			}
		}
		return nil
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func newEvidenceShowCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "show <evidence-id>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		service, err := openControlPlaneService(command)
		if err != nil {
			return err
		}
		record, found, err := service.FindEvidence(args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("unknown evidence record %q", args[0])
		}
		if asJSON {
			return writeLocalRecordJSON(command, record)
		}
		_, err = fmt.Fprintf(command.OutOrStdout(), "ID: %s\nPack: %s\nSource: %s\nOwner: %s\nType: %s\nURL: %s\nRetrieved: %s\nRevision: %s\nSHA-256: %s\nFact: %s\nVerification: %s\n", record.ID, record.PackID, record.SourceID, record.Owner, record.SourceType, record.CanonicalURL, record.RetrievedAt, record.DocumentRevision, record.ContentSHA256, record.ExtractedFact, record.VerificationID)
		return err
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func newVerificationCommand() *cobra.Command {
	command := &cobra.Command{Use: "verification", Short: "Inspect immutable local verification history", Args: cobra.NoArgs}
	command.AddCommand(newVerificationListCommand(), newVerificationShowCommand())
	return command
}

func newVerificationListCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		service, err := openControlPlaneService(command)
		if err != nil {
			return err
		}
		listing, err := service.Verifications()
		if err != nil {
			return err
		}
		if asJSON {
			return writeLocalRecordJSON(command, listing)
		}
		for _, record := range listing.Records {
			if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\t%d claim(s)\n", record.ID, record.RecordedAt, record.PackID, len(record.Claims)); err != nil {
				return err
			}
		}
		return nil
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func newVerificationShowCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "show <verification-id>", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		service, err := openControlPlaneService(command)
		if err != nil {
			return err
		}
		record, found, err := service.FindVerification(args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("unknown verification record %q", args[0])
		}
		if asJSON {
			return writeLocalRecordJSON(command, record)
		}
		_, err = fmt.Fprintf(command.OutOrStdout(), "ID: %s\nRecorded: %s\nScope: %s\nPack: %s\nClaims: %d\nParent: %s\n", record.ID, record.RecordedAt, record.Scope, record.PackID, len(record.Claims), record.ParentVerificationID)
		return err
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func openControlPlaneService(command *cobra.Command) (*controlplane.Service, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return controlplane.Open(command.Context(), cwd)
}

func writeLocalRecordJSON(command *cobra.Command, value any) error {
	encoder := json.NewEncoder(command.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
