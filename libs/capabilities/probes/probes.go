// Package probes contains ready-to-register [capabilities.DependencyProbe]
// constructors for the backing stores OpenFoundry services use most:
// Postgres (pgx), Cassandra/Scylla (gocql) and Kafka (segmentio/kafka-go).
//
// Lives in a sub-package on purpose: keeps `libs/capabilities` free of
// the heavy driver imports, so a service that only mounts the meta
// surface (no probes) doesn't pull pgx + gocql + kafka-go transitively.
//
// Wire-up shape (typical service boot):
//
//	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
//	caps.RegisterDependency(probes.Postgres("primary", pool))
//	caps.RegisterDependency(probes.Cassandra("ontology", session))
//	caps.RegisterDependency(probes.Kafka("data-bus", cfg.Kafka.Brokers))
//	caps.Mount(r)
//
// All probes are short-lived (default 1s timeout enforced by the
// capability registry) and read-only.
package probes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gocql/gocql"
	"github.com/jackc/pgx/v5/pgxpool"
	kafka "github.com/segmentio/kafka-go"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
)

// Postgres returns a probe that calls pgxpool.Pool.Ping. Pool may be
// nil — in that case the probe always reports degraded with a clear
// message, which is useful for services that boot in best-effort mode
// (e.g. without DATABASE_URL during local dev).
func Postgres(name string, pool *pgxpool.Pool) capabilities.DependencyProbe {
	return capabilities.DependencyProbe{
		Name: name,
		Kind: "postgres",
		Probe: func(ctx context.Context) error {
			if pool == nil {
				return errors.New("postgres pool is not configured")
			}
			return pool.Ping(ctx)
		},
	}
}

// Cassandra returns a probe that runs `SELECT now() FROM system.local`
// against the supplied gocql session. Session may be nil with the same
// semantics as [Postgres].
func Cassandra(name string, session *gocql.Session) capabilities.DependencyProbe {
	return capabilities.DependencyProbe{
		Name: name,
		Kind: "cassandra",
		Probe: func(ctx context.Context) error {
			if session == nil {
				return errors.New("cassandra session is not configured")
			}
			// gocql doesn't honour ctx for connection-level pings, but
			// query timeouts respect ctx through WithContext.
			iter := session.Query("SELECT now() FROM system.local").
				WithContext(ctx).
				Iter()
			if err := iter.Close(); err != nil {
				return fmt.Errorf("cassandra ping failed: %w", err)
			}
			return nil
		},
	}
}

// Kafka returns a probe that opens a TCP connection to the first
// broker in the list and asks the cluster for its broker metadata.
// Treats an empty broker slice as a configuration error.
func Kafka(name string, brokers []string) capabilities.DependencyProbe {
	return capabilities.DependencyProbe{
		Name: name,
		Kind: "kafka",
		Probe: func(ctx context.Context) error {
			if len(brokers) == 0 {
				return errors.New("no kafka brokers configured")
			}
			d := &kafka.Dialer{Timeout: deadlineFromCtx(ctx, 800*time.Millisecond)}
			conn, err := d.DialContext(ctx, "tcp", brokers[0])
			if err != nil {
				return fmt.Errorf("dial %s: %w", brokers[0], err)
			}
			defer conn.Close()
			if _, err := conn.Brokers(); err != nil {
				return fmt.Errorf("metadata: %w", err)
			}
			return nil
		},
	}
}

// HTTP returns a probe that issues a GET against `url` and accepts
// any 2xx/3xx as healthy. `kind` lets callers report a richer label
// than "http" (e.g. "lakekeeper", "opa", "jwks") in /_meta/health.
func HTTP(name, kind, url string) capabilities.DependencyProbe {
	if kind == "" {
		kind = "http"
	}
	return capabilities.DependencyProbe{
		Name: name,
		Kind: capabilities.DependencyKind(kind),
		Probe: func(ctx context.Context) error {
			if url == "" {
				return errors.New("no url configured")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := httpProbeClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("http %s", resp.Status)
			}
			return nil
		},
	}
}

var httpProbeClient = &http.Client{Timeout: 2 * time.Second}

func deadlineFromCtx(ctx context.Context, fallback time.Duration) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			return d
		}
	}
	return fallback
}

// PythonSidecar reports whether the pipeline-build-service Python runtime
// binary is configured. The probe is intentionally configuration-based: the
// actual manager startup path owns process liveness, while capabilities need a
// cheap, deterministic declaration of whether transform runtime capability can
// be published by this service instance.
func PythonSidecar(name, binaryPath string) capabilities.DependencyProbe {
	return capabilities.DependencyProbe{
		Name:            name,
		Kind:            "runtime",
		StatusOnSuccess: "available",
		StatusOnError:   "unavailable",
		Probe: func(context.Context) error {
			if binaryPath == "" {
				return errors.New("PYTHON_SIDECAR_BINARY is not configured")
			}
			return nil
		},
	}
}
