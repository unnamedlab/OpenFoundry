//go:build !unix

package authmw

import "os"

// createDirAllSecure mirrors the Rust `cfg(not(unix))` fallback —
// regular MkdirAll without explicit perms (the host's umask
// applies).
func createDirAllSecure(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// writeSecure mirrors the Rust `cfg(not(unix))` fallback — best-
// effort fs::write; perms are not enforced because the platform
// has no notion of POSIX file modes.
func writeSecure(path string, contents []byte) error {
	return os.WriteFile(path, contents, 0o600)
}
