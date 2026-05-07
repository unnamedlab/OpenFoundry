package pythonsidecar

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pb "github.com/openfoundry/openfoundry-go/libs/proto-gen/runtime"
)

// Manager owns one openfoundry-pyruntime subprocess and the gRPC
// connection to it. Construct with [New], call Start, then access the
// typed client via [Manager.Client] or use the convenience wrappers
// [Manager.ExecuteInline], [Manager.ExecutePipeline], etc.
type Manager struct {
	cfg Config
	log *slog.Logger

	mu        sync.RWMutex
	cmd       *exec.Cmd
	conn      *grpc.ClientConn
	client    pb.PythonRuntimeServiceClient
	health    healthpb.HealthClient
	supervise context.CancelFunc
	stopped   atomic.Bool
	wg        sync.WaitGroup
}

// New constructs a manager. Logger may be nil (slog.Default is used).
func New(cfg Config, logger *slog.Logger) (*Manager, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, log: logger}, nil
}

// Start spawns the sidecar, waits for the gRPC health check to report
// SERVING, and launches the supervisor goroutine. Subsequent calls
// after a successful start return ErrAlreadyStarted.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.cmd != nil {
		m.mu.Unlock()
		return ErrAlreadyStarted
	}
	m.mu.Unlock()

	if err := m.spawnAndConnect(ctx); err != nil {
		return err
	}
	superviseCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.supervise = cancel
	m.mu.Unlock()
	m.wg.Add(1)
	go m.superviseLoop(superviseCtx)
	return nil
}

// Stop terminates the sidecar and closes the connection. Safe to call
// multiple times.
func (m *Manager) Stop(ctx context.Context) error {
	if !m.stopped.CompareAndSwap(false, true) {
		return nil
	}
	m.mu.Lock()
	cancel := m.supervise
	m.supervise = nil
	cmd := m.cmd
	conn := m.conn
	m.cmd = nil
	m.conn = nil
	m.client = nil
	m.health = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.wg.Wait()

	if cmd != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		case <-ctx.Done():
			_ = cmd.Process.Kill()
		}
	}
	if conn != nil {
		_ = conn.Close()
	}
	_ = os.Remove(m.cfg.socketPath())
	return nil
}

// Client returns the gRPC stub. nil if Start has not run.
func (m *Manager) Client() pb.PythonRuntimeServiceClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// SocketPath is the address the sidecar is listening on.
func (m *Manager) SocketPath() string { return m.cfg.socketPath() }

// HardTimeout returns the configured per-call timeout.
func (m *Manager) HardTimeout() time.Duration { return m.cfg.HardCallTimeout }

func (m *Manager) spawnAndConnect(ctx context.Context) error {
	socket := m.cfg.socketPath()
	_ = os.Remove(socket)

	args := append([]string{"--bind", "unix:" + socket}, m.cfg.Args...)
	cmd := exec.Command(m.cfg.BinaryPath, args...)
	cmd.Env = append(os.Environ(), m.cfg.Env...)
	if m.cfg.Stdout != nil {
		cmd.Stdout = m.cfg.Stdout
	}
	if m.cfg.Stderr != nil {
		cmd.Stderr = m.cfg.Stderr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn sidecar: %w", err)
	}

	conn, err := grpc.NewClient(
		"unix:"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("dial sidecar: %w", err)
	}

	healthClient := healthpb.NewHealthClient(conn)
	deadline := time.Now().Add(m.cfg.StartupTimeout)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		resp, probeErr := healthClient.Check(probeCtx, &healthpb.HealthCheckRequest{})
		cancel()
		if probeErr == nil && resp.Status == healthpb.HealthCheckResponse_SERVING {
			break
		}
		if time.Now().After(deadline) {
			_ = conn.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("sidecar did not become healthy within %s: %w", m.cfg.StartupTimeout, probeErr)
		}
		select {
		case <-ctx.Done():
			_ = conn.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

	m.mu.Lock()
	m.cmd = cmd
	m.conn = conn
	m.client = pb.NewPythonRuntimeServiceClient(conn)
	m.health = healthClient
	m.mu.Unlock()
	m.log.Info("python sidecar ready", "binary", m.cfg.BinaryPath, "socket", socket, "pid", cmd.Process.Pid)
	return nil
}

func (m *Manager) superviseLoop(ctx context.Context) {
	defer m.wg.Done()
	failures := 0
	backoff := time.Second
	ticker := time.NewTicker(m.cfg.HealthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if m.stopped.Load() {
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		resp, err := m.health.Check(probeCtx, &healthpb.HealthCheckRequest{})
		cancel()
		if err == nil && resp.Status == healthpb.HealthCheckResponse_SERVING {
			failures = 0
			backoff = time.Second
			continue
		}
		failures++
		m.log.Warn("python sidecar health probe failed", "error", err, "consecutive", failures)
		if failures < m.cfg.HealthFailuresBeforeRestart {
			continue
		}
		m.log.Warn("python sidecar restart triggered", "backoff", backoff)
		time.Sleep(backoff)
		if backoff < m.cfg.MaxRestartBackoff {
			backoff *= 2
		}
		m.restart(ctx)
		failures = 0
	}
}

func (m *Manager) restart(ctx context.Context) {
	m.mu.Lock()
	cmd := m.cmd
	conn := m.conn
	m.cmd = nil
	m.conn = nil
	m.client = nil
	m.health = nil
	m.mu.Unlock()
	if cmd != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if err := m.spawnAndConnect(ctx); err != nil {
		m.log.Error("python sidecar restart failed", "error", err)
	}
}

// ErrAlreadyStarted is returned when Start is called twice.
var ErrAlreadyStarted = errors.New("python-sidecar: already started")
