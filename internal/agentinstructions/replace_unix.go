//go:build !windows

package agentinstructions

import "os"

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}
