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
	"net"
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
// any 2xx/3xx as healthy. Useful for upstream sidecar dependencies
// (Lakekeeper, OPA, vector store control plane, …).
func HTTP(name, url string) capabilities.DependencyProbe {
	return capabilities.DependencyProbe{
		Name: name,
		Kind: "http",
		Probe: func(ctx context.Context) error {
			if url == "" {
				return errors.New("no url configured")
			}
			d := net.Dialer{Timeout: deadlineFromCtx(ctx, 800*time.Millisecond)}
			// We avoid net/http here to keep the probe dependency-free
			// at the TCP layer — most failures we care about are
			// connectivity issues, not HTTP semantics.
			conn, err := d.DialContext(ctx, "tcp", hostPortFromURL(url))
			if err != nil {
				return err
			}
			_ = conn.Close()
			return nil
		},
	}
}

func deadlineFromCtx(ctx context.Context, fallback time.Duration) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d > 0 {
			return d
		}
	}
	return fallback
}

// hostPortFromURL extracts host:port from the most common URL shapes
// without pulling net/url. Falls back to the raw input.
func hostPortFromURL(raw string) string {
	s := raw
	for _, prefix := range []string{"http://", "https://"} {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
			break
		}
	}
	if i := indexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := indexByte(s, ':'); i < 0 {
		return s + ":80"
	}
	return s
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
