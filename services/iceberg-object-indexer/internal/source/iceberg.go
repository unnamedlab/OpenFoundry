package source

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/iceberg-go"
	"github.com/apache/iceberg-go/catalog"
	"github.com/apache/iceberg-go/catalog/rest"
	icebergio "github.com/apache/iceberg-go/io"
	"github.com/apache/iceberg-go/table"
)

// IcebergConfig configures the REST catalog connection and the table
// being scanned. Empty optional fields are simply not forwarded as
// catalog options.
type IcebergConfig struct {
	CatalogName       string
	CatalogURI        string
	Warehouse         string
	Credential        string
	OAuthTokenURI     string
	OAuthScope        string
	SourceTable       string
}

// Iceberg is a Source backed by apache/iceberg-go's REST catalog.
type Iceberg struct {
	cfg   IcebergConfig
	cat   catalog.Catalog
	ident table.Identifier
}

// OpenIceberg resolves the REST catalog and parses the table
// identifier. The catalog connection is kept and reused across calls
// to Scan; release it with Close.
func OpenIceberg(ctx context.Context, cfg IcebergConfig) (*Iceberg, error) {
	ident, err := parseTableIdent(cfg.CatalogName, cfg.SourceTable)
	if err != nil {
		return nil, err
	}
	opts, err := restOptions(cfg)
	if err != nil {
		return nil, err
	}
	cat, err := rest.NewCatalog(ctx, cfg.CatalogName, cfg.CatalogURI, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect rest catalog at %s: %w", cfg.CatalogURI, err)
	}
	return &Iceberg{cfg: cfg, cat: cat, ident: ident}, nil
}

func restOptions(cfg IcebergConfig) ([]rest.Option, error) {
	// Lakekeeper advertises `s3.remote-signing-enabled=true` and a
	// `s3.signer-uri` in every table load response. apache/iceberg-go
	// v0.5.0's gocloud S3 IO adapter does not implement remote signing
	// (returns "remote S3 request signing is not supported"). Override
	// it client-side so the AWS SDK signs requests locally with the
	// AWS_* env credentials the pod ships with.
	opts := []rest.Option{
		rest.WithAdditionalProps(iceberg.Properties{
			icebergio.S3RemoteSigningEnabled: "false",
		}),
	}
	if cfg.Warehouse != "" {
		opts = append(opts, rest.WithWarehouseLocation(cfg.Warehouse))
	}
	if cfg.Credential != "" {
		opts = append(opts, rest.WithCredential(cfg.Credential))
	}
	if cfg.OAuthTokenURI != "" {
		u, err := url.Parse(cfg.OAuthTokenURI)
		if err != nil {
			return nil, fmt.Errorf("parse oauth-token-uri: %w", err)
		}
		opts = append(opts, rest.WithAuthURI(u))
	}
	if cfg.OAuthScope != "" {
		opts = append(opts, rest.WithScope(cfg.OAuthScope))
	}
	return opts, nil
}

// Close releases the underlying REST catalog connection. apache/iceberg-go
// does not expose an explicit close, so this is a no-op today —
// kept for forward compatibility and to satisfy the Source contract.
func (s *Iceberg) Close() error { return nil }

// Scan plans the read against the current snapshot and returns a row
// iterator. Limit is mapped to Iceberg's row-limit scan option; the
// scan honours ctx cancellation.
func (s *Iceberg) Scan(ctx context.Context, limit int64) (iter.Seq2[Row, error], error) {
	tbl, err := s.cat.LoadTable(ctx, s.ident)
	if err != nil {
		return nil, fmt.Errorf("load table %v: %w", s.ident, err)
	}
	scan := tbl.Scan()
	if limit > 0 {
		scan = scan.UseRowLimit(limit)
	}
	schema, batches, err := scan.ToArrowRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("plan scan for %v: %w", s.ident, err)
	}
	return func(yield func(Row, error) bool) {
		for batch, berr := range batches {
			if berr != nil {
				yield(nil, berr)
				return
			}
			n := int(batch.NumRows())
			for r := 0; r < n; r++ {
				row := batchRowToMap(schema, batch, r)
				if !yield(row, nil) {
					batch.Release()
					return
				}
			}
			batch.Release()
		}
	}, nil
}

// parseTableIdent normalises a Spark-style table reference
// "catalog.namespace[.sub].table" into the namespace/table tuple
// iceberg-go expects (catalog name is already known by the
// Catalog handle). When the first component matches CatalogName it
// is stripped; otherwise the whole reference is treated as the
// namespace path + final table.
func parseTableIdent(catalogName, ref string) (table.Identifier, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("source-table must not be empty")
	}
	parts := strings.Split(ref, ".")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("source-table %q: empty path component", ref)
		}
	}
	if len(parts) >= 2 && parts[0] == catalogName {
		parts = parts[1:]
	}
	if len(parts) < 2 {
		return nil, fmt.Errorf("source-table %q must be at least namespace.table once the catalog prefix is stripped", ref)
	}
	return table.Identifier(parts), nil
}

// batchRowToMap reads row `r` of `batch` into a column-name → value
// map suitable for json.Marshal. Uses Arrow's GetOneForMarshal so
// every type (timestamps, decimals, lists, structs) is rendered the
// same way Arrow's own JSON encoder would.
func batchRowToMap(schema *arrow.Schema, batch arrow.RecordBatch, r int) Row {
	n := schema.NumFields()
	row := make(Row, n)
	for c := 0; c < n; c++ {
		name := schema.Field(c).Name
		col := batch.Column(c)
		if col.IsNull(r) {
			row[name] = nil
			continue
		}
		row[name] = col.GetOneForMarshal(r)
	}
	return row
}
