package cassandrakernel

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// Cluster bundles the knobs every OpenFoundry service uses to dial
// Cassandra/Scylla. Mirrors the Rust ClusterConfig in spirit; uses
// gocql's ClusterConfig under the hood.
type Cluster struct {
	Hosts         []string
	Keyspace      string // optional — leave empty when bootstrapping (we run KEYSPACE-creating DDL separately)
	Username      string
	Password      string
	Consistency   gocql.Consistency
	DialTimeout   time.Duration
	NumConns      int
	Datacenter    string // for token-aware + DC-aware routing
	DisableInit   bool   // skip auto-discovery (useful for testcontainers)
}

// FromEnv builds a Cluster from the Rust-aligned env contract:
//
//	CASSANDRA_CONTACT_POINTS  CSV "host:port,host2:port2"
//	CASSANDRA_KEYSPACE        e.g. "auth_runtime"
//	CASSANDRA_USERNAME / CASSANDRA_PASSWORD  (optional)
//	CASSANDRA_LOCAL_DC        e.g. "dc1"
//	CASSANDRA_CONSISTENCY     QUORUM (default LOCAL_QUORUM)
//
// All variables match the Rust workspace verbatim so a single Helm
// values.yaml drives both implementations during cutover.
func FromEnv() (*Cluster, error) {
	cps := os.Getenv("CASSANDRA_CONTACT_POINTS")
	if cps == "" {
		return nil, &MissingEnvError{Key: "CASSANDRA_CONTACT_POINTS"}
	}
	hosts := splitCSV(cps)
	if len(hosts) == 0 {
		return nil, fmt.Errorf("CASSANDRA_CONTACT_POINTS resolved to no hosts: %q", cps)
	}
	c := &Cluster{
		Hosts:       hosts,
		Keyspace:    os.Getenv("CASSANDRA_KEYSPACE"),
		Username:    os.Getenv("CASSANDRA_USERNAME"),
		Password:    os.Getenv("CASSANDRA_PASSWORD"),
		Datacenter:  defaultStr(os.Getenv("CASSANDRA_LOCAL_DC"), "dc1"),
		DialTimeout: 5 * time.Second,
		NumConns:    2,
		Consistency: parseConsistency(os.Getenv("CASSANDRA_CONSISTENCY")),
	}
	return c, nil
}

// Connect returns a connected gocql session.
//
// The caller owns Session.Close — typically deferred from main.
func (c *Cluster) Connect() (*gocql.Session, error) {
	cfg := gocql.NewCluster(c.Hosts...)
	cfg.Keyspace = c.Keyspace
	cfg.Consistency = c.Consistency
	cfg.Timeout = c.DialTimeout
	cfg.NumConns = c.NumConns
	cfg.DisableInitialHostLookup = c.DisableInit
	if c.Username != "" {
		cfg.Authenticator = gocql.PasswordAuthenticator{
			Username: c.Username,
			Password: c.Password,
		}
	}
	if c.Datacenter != "" {
		cfg.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
			gocql.DCAwareRoundRobinPolicy(c.Datacenter),
		)
	}
	return cfg.CreateSession()
}

// MissingEnvError signals a required env var was unset.
type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

// IsMissingEnv reports whether err is a MissingEnvError.
func IsMissingEnv(err error) bool {
	var me *MissingEnvError
	return errors.As(err, &me)
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func splitCSV(v string) []string {
	out := []string{}
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseConsistency(v string) gocql.Consistency {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ONE":
		return gocql.One
	case "QUORUM":
		return gocql.Quorum
	case "ALL":
		return gocql.All
	case "LOCAL_ONE":
		return gocql.LocalOne
	case "LOCAL_QUORUM", "":
		return gocql.LocalQuorum
	case "EACH_QUORUM":
		return gocql.EachQuorum
	default:
		return gocql.LocalQuorum
	}
}
