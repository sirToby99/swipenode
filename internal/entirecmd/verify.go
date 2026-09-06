package entirecmd

import (
	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/sirToby99/swipenode/internal/entireprovenance"
	"github.com/sirToby99/swipenode/internal/verifycmd"
	"github.com/spf13/cobra"
)

func newVerifyCommand(deps Dependencies) *cobra.Command {
	enricher := deps.ProvenanceEnricher
	if enricher == nil {
		enricher = entireprovenance.New(entireprovenance.Dependencies{Getenv: deps.Getenv, LookPath: deps.LookPath})
	}
	return verifycmd.NewCommand(verifycmd.Dependencies{
		Getwd: deps.Getwd,
		ResolveRoot: func(cwd string) (string, error) {
			return entirectx.ResolveRepoRoot(entirectx.FromLookup(deps.Getenv), cwd)
		},
		Lookup:             deps.Lookup,
		ProvenanceEnricher: enricher,
	})
}
