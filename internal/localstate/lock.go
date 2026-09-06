// Package localstate coordinates repository-local SwipeNode mutations.
package localstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Lock struct {
	dir      string
	released bool
}

type lockOwner struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}

// Acquire creates an atomic directory lock. A surviving lock is deliberately
// not stolen automatically because process liveness is not portable enough to
// prove that doing so is safe.
func Acquire(stateDir, name string) (*Lock, error) {
	if name == "" || filepath.Base(name) != name {
		return nil, fmt.Errorf("invalid local state lock name")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create local state directory: %w", err)
	}
	dir := filepath.Join(stateDir, name+".lock")
	if err := os.Mkdir(dir, 0o700); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%s is already in progress", name)
		}
		return nil, fmt.Errorf("acquire %s lock: %w", name, err)
	}
	owner, _ := json.Marshal(lockOwner{PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), append(owner, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write %s lock owner: %w", name, err)
	}
	return &Lock{dir: dir}, nil
}

func (lock *Lock) Release() error {
	if lock == nil || lock.released {
		return nil
	}
	lock.released = true
	if err := os.RemoveAll(lock.dir); err != nil {
		return fmt.Errorf("release local state lock: %w", err)
	}
	return nil
}
