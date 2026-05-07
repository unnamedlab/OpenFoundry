//go:build unix

package authmw

import (
	"errors"
	"io/fs"
	"os"
)

// createDirAllSecure creates `dir` (recursively) with mode 0700,
// matching the Rust `create_dir_all_secure` Unix branch.
//
// Existing directories are left untouched (the Rust source
// short-circuits via `dir.exists()`).
func createDirAllSecure(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return &os.PathError{Op: "stat", Path: dir, Err: errors.New("not a directory")}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

// writeSecure writes `contents` to `path` using O_CREATE|O_EXCL
// with mode 0600 — fails if the file already exists. Matches the
// Rust `create_new(true).mode(0o600)` semantics.
func writeSecure(path string, contents []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(contents); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
