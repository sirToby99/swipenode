package main

import (
	"os"

	"github.com/sirToby99/swipenode/internal/buildinfo"
	"github.com/sirToby99/swipenode/internal/entirecmd"
)

func main() {
	os.Exit(entirecmd.Execute(entirecmd.BuildInfo{
		Version: buildinfo.Version, Commit: buildinfo.Commit, BuildDate: buildinfo.BuildDate,
	}))
}
