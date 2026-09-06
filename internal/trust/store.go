package trust

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	dir string
	now func() time.Time
}

func Open(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, "trust"), now: time.Now}
}
func (store *Store) Load(purpose Purpose) (Document, bool, error) {
	path := filepath.Join(store.dir, string(purpose)+".json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Document{}, false, nil
	}
	if err != nil {
		return Document{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Document{}, false, fmt.Errorf("unsafe trust metadata file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, false, err
	}
	doc, err := Decode(data)
	return doc, err == nil, err
}
func (store *Store) InstallInitial(next Document, bootstrap []byte) error {
	key, err := ParsePublicKey(string(bootstrap))
	if err != nil {
		return err
	}
	if err := VerifyInitial(next, key, next.Purpose, store.now().UTC()); err != nil {
		return err
	}
	if _, ok, err := store.Load(next.Purpose); err != nil {
		return err
	} else if ok {
		return fmt.Errorf("trust metadata already initialized")
	}
	return store.save(next)
}
func (store *Store) Update(next Document) error {
	previous, ok, err := store.Load(next.Purpose)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("trust metadata is not initialized")
	}
	if err := VerifyUpdate(previous, next, store.now().UTC()); err != nil {
		return err
	}
	return store.save(next)
}
func (store *Store) save(doc Document) error {
	data, err := Encode(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(store.dir, ".trust-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(store.dir, string(doc.Purpose)+".json"))
}
