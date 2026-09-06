package cmd

import "github.com/sirToby99/swipenode/internal/knowledgecmd"

func init() {
	deps := knowledgecmd.DefaultDependencies()
	rootCmd.AddCommand(knowledgecmd.NewKnowledgeCommand(deps), knowledgecmd.NewAuditCommand(deps))
}
