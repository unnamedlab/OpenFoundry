package runner

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Args is the indexer's CLI surface. Defaults match the Scala
// `IcebergToObjectStoreIndexer` so the SparkApplication CR can be
// swapped for a Kubernetes Job without operator-facing changes.
type Args struct {
	SourceTable       string
	TargetTenant      string
	TargetTypeID      string
	IDColumn          string
	ObjectDatabaseURL string
	InternalToken     string
	CatalogName       string
	CatalogURI        string
	CatalogWarehouse  string
	CatalogCredential string
	OAuthTokenURI     string
	OAuthScope        string
	Limit             int64
	Smoke             bool
	HealthAddr        string
	LogFormat         string
}

// ParseArgs reads `raw` (typically os.Args[1:]) into an Args, applying
// defaults, then validates required fields. `errOut` receives the
// flag-package usage message on parse failure.
func ParseArgs(name string, raw []string, errOut io.Writer) (Args, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)

	a := defaultArgs()
	fs.StringVar(&a.SourceTable, "source-table", a.SourceTable, "iceberg ref, e.g. lakekeeper.default.transactions_clean")
	fs.StringVar(&a.TargetTenant, "target-tenant", a.TargetTenant, "tenant for the object-database PUT path")
	fs.StringVar(&a.TargetTypeID, "target-type-id", a.TargetTypeID, "ontology object_type_id (UUID)")
	fs.StringVar(&a.IDColumn, "id-column", a.IDColumn, "row column carrying the object id")
	fs.StringVar(&a.ObjectDatabaseURL, "object-database-url", a.ObjectDatabaseURL, "base URL for object-database-service")
	fs.StringVar(&a.InternalToken, "internal-token", a.InternalToken, "optional X-Internal-Token header value")
	fs.StringVar(&a.CatalogName, "catalog", a.CatalogName, "iceberg catalog name registered with the REST client")
	fs.StringVar(&a.CatalogURI, "catalog-uri", a.CatalogURI, "iceberg REST catalog base URL")
	fs.StringVar(&a.CatalogWarehouse, "catalog-warehouse", a.CatalogWarehouse, "iceberg REST warehouse identifier (e.g. openfoundry)")
	fs.StringVar(&a.CatalogCredential, "catalog-credential", a.CatalogCredential, "optional 'user:secret' credential for the REST catalog")
	fs.StringVar(&a.OAuthTokenURI, "oauth-token-uri", a.OAuthTokenURI, "optional OAuth2 token endpoint for the REST catalog")
	fs.StringVar(&a.OAuthScope, "oauth-scope", a.OAuthScope, "optional OAuth2 scope for the REST catalog token request")
	fs.Int64Var(&a.Limit, "limit", a.Limit, "max rows to PUT (0 = unbounded)")
	fs.BoolVar(&a.Smoke, "smoke", a.Smoke, "validate args and exit; do not read iceberg or PUT objects")
	fs.StringVar(&a.HealthAddr, "health-addr", a.HealthAddr, "bind address for /healthz and /metrics")
	fs.StringVar(&a.LogFormat, "log-format", a.LogFormat, "log format: 'text' (default) or 'json'")

	if err := fs.Parse(raw); err != nil {
		return Args{}, err
	}
	return a, a.Validate()
}

func defaultArgs() Args {
	return Args{
		TargetTenant:      "default",
		ObjectDatabaseURL: "http://object-database-service.openfoundry.svc:8080",
		CatalogName:       "lakekeeper",
		CatalogURI:        "http://lakekeeper.lakekeeper.svc:8181/catalog",
		HealthAddr:        "0.0.0.0:9090",
		LogFormat:         "text",
	}
}

// Validate enforces the same required-flag set as the Scala CLI.
func (a Args) Validate() error {
	var missing []string
	if strings.TrimSpace(a.SourceTable) == "" {
		missing = append(missing, "--source-table")
	}
	if strings.TrimSpace(a.TargetTypeID) == "" {
		missing = append(missing, "--target-type-id")
	}
	if strings.TrimSpace(a.IDColumn) == "" {
		missing = append(missing, "--id-column")
	}
	if strings.TrimSpace(a.CatalogName) == "" {
		missing = append(missing, "--catalog")
	}
	if strings.TrimSpace(a.CatalogURI) == "" {
		missing = append(missing, "--catalog-uri")
	}
	if strings.TrimSpace(a.ObjectDatabaseURL) == "" {
		missing = append(missing, "--object-database-url")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if a.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", a.Limit)
	}
	switch a.LogFormat {
	case "", "text", "json":
	default:
		return fmt.Errorf("--log-format must be 'text' or 'json', got %q", a.LogFormat)
	}
	return nil
}
