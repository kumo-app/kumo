package core

import (
	"os"
	"path/filepath"
	"time"
)

// WriteFileAtomic writes data to a temporary file beside path, then renames
// it over path, so that a crash never leaves a half-written file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// KeepCorrupt copies a file that could not be read to a name of its own
// beside it, returning that name.
func KeepCorrupt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	kept := path + ".corrupt-" + time.Now().Format("20060102-150405")
	return kept, WriteFileAtomic(kept, data, 0o600)
}
