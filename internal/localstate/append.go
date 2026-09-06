package localstate

import (
	"fmt"
	"os"
)

// OpenAppendOnly opens one local append-only log without following a symbolic
// link. The second identity check closes the lstat/open race before callers
// write any bytes to the descriptor.
func OpenAppendOnly(path string, mode os.FileMode) (*os.File, error) {
	before, err := os.Lstat(path)
	if err == nil && !before.Mode().IsRegular() {
		return nil, fmt.Errorf("append-only state path is not a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return nil, err
	}
	after, pathErr := os.Lstat(path)
	opened, fileErr := file.Stat()
	if pathErr != nil || fileErr != nil || !after.Mode().IsRegular() || !opened.Mode().IsRegular() || !os.SameFile(after, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("append-only state path changed or is unsafe")
	}
	return file, nil
}
