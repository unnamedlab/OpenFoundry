package pythonsidecar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Config controls how the manager spawns and supervises the sidecar.
//
// Defaults are chosen so that constructing `Config{BinaryPath: "..."}`
// is enough for most callers; everything else has a sane default
// applied in (*Config).normalize.
type Config struct {
	// BinaryPath is the absolute path to the openfoundry-pyruntime
	// executable. Required.
	BinaryPath string

	// Args are extra CLI args appended after `--bind <socket>`. The
	// manager always passes `--bind` so callers must not set it again.
	Args []string

	// Env is appended to the inherited process env. Use this to point
	// the sidecar at a particular venv (e.g. PATH=...) or pass
	// PYRUNTIME_LOG=DEBUG.
	Env []string

	// SocketDir is where the manager creates the Unix domain socket.
	// Defaults to os.TempDir().
	SocketDir string

	// SocketName overrides the socket basename. Default is a unique
	// "openfoundry-pyruntime-<uuid>.sock".
	SocketName string

	// StartupTimeout caps how long Start waits for the sidecar to
	// become healthy. Default 10s.
	StartupTimeout time.Duration

	// HealthInterval is how often the supervisor probes the sidecar.
	// Default 5s.
	HealthInterval time.Duration

	// HealthFailuresBeforeRestart is how many consecutive failed health
	// probes trigger a restart. Default 3.
	HealthFailuresBeforeRestart int

	// MaxRestartBackoff caps the exponential backoff between restarts.
	// Default 30s.
	MaxRestartBackoff time.Duration

	// HardCallTimeout is the maximum wall time any single Execute*
	// RPC may take. Default 60s.
	HardCallTimeout time.Duration

	// Stdout / Stderr are forwarded from the subprocess. Nil means
	// /dev/null (the manager opens it).
	Stdout *os.File
	Stderr *os.File
}

func (c *Config) normalize() error {
	if c.BinaryPath == "" {
		return errors.New("python-sidecar: BinaryPath is required")
	}
	if _, err := os.Stat(c.BinaryPath); err != nil {
		return fmt.Errorf("python-sidecar: BinaryPath %q: %w", c.BinaryPath, err)
	}
	if c.SocketDir == "" {
		c.SocketDir = os.TempDir()
	}
	if c.SocketName == "" {
		// Keep the basename short — macOS sun_path is capped at 104 bytes
		// (sockaddr_un.sun_path) including the null terminator and we lose
		// most of that to /var/folders/...
		short := uuid.NewString()
		c.SocketName = "of-pyrt-" + short[:8] + ".sock"
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = 10 * time.Second
	}
	if c.HealthInterval == 0 {
		c.HealthInterval = 5 * time.Second
	}
	if c.HealthFailuresBeforeRestart == 0 {
		c.HealthFailuresBeforeRestart = 3
	}
	if c.MaxRestartBackoff == 0 {
		c.MaxRestartBackoff = 30 * time.Second
	}
	if c.HardCallTimeout == 0 {
		c.HardCallTimeout = 60 * time.Second
	}
	return nil
}

func (c *Config) socketPath() string {
	return filepath.Join(c.SocketDir, c.SocketName)
}
