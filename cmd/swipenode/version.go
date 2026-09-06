package cmd

import "github.com/sirToby99/swipenode/internal/buildinfo"

func buildVersion() string {
	return buildinfo.String()
}
