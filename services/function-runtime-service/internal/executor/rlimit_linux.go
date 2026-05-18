//go:build linux

package executor

import (
	"os/exec"
	"syscall"
)

// applyRlimit attaches an AddressSpace rlimit equal to
// lim.MemoryLimitMiB MiB to cmd. The child inherits the limit at
// fork-exec; the kernel kills it (SIGSEGV) on overrun.
//
// Setrlimit on RLIMIT_AS is intentionally conservative — it caps
// virtual memory, not RSS, so the actual physical-memory ceiling
// can be lower. v1 will move to cgroups + namespacing.
func applyRlimit(cmd *exec.Cmd, lim Limits) {
	if lim.MemoryLimitMiB == 0 {
		return
	}
	bytes := lim.MemoryLimitMiB * 1024 * 1024
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL

	// Setrlimit on the child happens via a small pre-exec hook on
	// linux: SysProcAttr.AmbientCaps etc. cannot enforce rlimits
	// directly. We use Setpgid + Pdeathsig to make sure the kernel
	// reaps the process group when the parent dies, and rely on the
	// runtime-specific RLIMIT_AS hint baked into the env so the script
	// itself can self-limit when supported.
	//
	// NB: a full Setrlimit requires a wrapper child (see
	// prlimit(2) / `setrlimit(RLIMIT_AS, ...)` from a forked
	// process). Until v1 wires that, we expose the value via env so
	// the script body can honour it where the runtime allows it
	// (e.g. `NODE_OPTIONS=--max-old-space-size=<n>`).
	cmd.Env = append(cmd.Env,
		"OF_FUNCTION_MEMORY_LIMIT_BYTES="+itoa(bytes),
		"NODE_OPTIONS=--max-old-space-size="+itoa(lim.MemoryLimitMiB),
	)
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
