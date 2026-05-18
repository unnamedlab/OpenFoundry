//go:build !linux

package executor

import "os/exec"

// applyRlimit is a no-op outside linux. macOS / windows dev hosts can
// still exercise the timeout path; the memory ceiling is only
// enforced in linux containers.
func applyRlimit(_ *exec.Cmd, _ Limits) {}
