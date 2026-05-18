// Package providers wires the concrete [pipelineruntime.Reader] and
// [pipelineruntime.Writer] implementations the pipeline-runner uses
// in production: an apache/iceberg-go-backed reader (Phase A pattern,
// pin + gocloud + S3 remote-signing override) and an HTTP-adapter
// writer that posts to `iceberg-catalog-service`'s
// `/openfoundry/iceberg/v1/append` endpoint (Phase B pattern).
package providers

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

	pipelineruntime "github.com/openfoundry/openfoundry-go/libs/pipeline-runtime"
)

// IcebergReaderConfig configures the REST catalog connection plus
// the auth knobs the Lakekeeper-flavoured catalog wants. Empty
// optional fields are simply not forwarded as catalog options.
type IcebergReaderConfig struct {
	// CatalogName is the catalog handle the Plan's read_table ops
	// must match. A Scan against a different name returns
	// ErrUnknownCatalog so half-migrated clusters fail loudly.
	CatalogName string
	// CatalogURI is the REST catalog base URL (e.g. Lakekeeper).
	CatalogURI string
	// Warehouse — Iceberg warehouse identifier (optional).
	Warehouse string
	// Credential — basic-auth credential ("user:secret") forwarded
	// to the REST catalog. Optional.
	Credential string
	// OAuthTokenURI — OAuth2 token endpoint for the REST catalog.
	// Optional.
	OAuthTokenURI string
	// OAuthScope — OAuth2 scope. Optional.
	OAuthScope string
}

// IcebergReader is the pipelineruntime.Reader implementation backed
// by apache/iceberg-go. Phase A's indexer source baseline plus the
// per-Scan dispatch the Plan-driven runtime needs.
type IcebergReader struct {
	cfg IcebergReaderConfig
	cat catalog.Catalog
}

// OpenIcebergReader builds the REST catalog connection. The
// connection is kept on the returned reader and reused across
// Scan calls; Close releases it.
//
// Discovered behaviour from Phase A: Lakekeeper advertises
// `s3.remote-signing-enabled=true` plus an `s3.signer-uri` on every
// LoadTable response and apache/iceberg-go v0.5.0's gocloud S3
// adapter does not implement remote signing. The override below
// turns it off so the AWS SDK signs requests locally with the AWS_*
// env credentials.
func OpenIcebergReader(ctx context.Context, cfg IcebergReaderConfig) (*IcebergReader, error) {
	if strings.TrimSpace(cfg.CatalogName) == "" {
		return nil, fmt.Errorf("iceberg reader: catalog name is required")
	}
	if strings.TrimSpace(cfg.CatalogURI) == "" {
		return nil, fmt.Errorf("iceberg reader: catalog URI is required")
	}
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
			return nil, fmt.Errorf("iceberg reader: parse oauth-token-uri: %w", err)
		}
		opts = append(opts, rest.WithAuthURI(u))
	}
	if cfg.OAuthScope != "" {
		opts = append(opts, rest.WithScope(cfg.OAuthScope))
	}
	cat, err := rest.NewCatalog(ctx, cfg.CatalogName, cfg.CatalogURI, opts...)
	if err != nil {
		return nil, fmt.Errorf("iceberg reader: connect REST catalog at %s: %w", cfg.CatalogURI, err)
	}
	return &IcebergReader{cfg: cfg, cat: cat}, nil
}

// ErrUnknownCatalog is returned when Scan is called with a catalog
// name the reader was not configured for.
var ErrUnknownCatalog = fmt.Errorf("iceberg reader: unknown catalog")

// Close releases the catalog connection. apache/iceberg-go does not
// expose an explicit close, so this is a no-op today — kept for
// forward compatibility with the pipelineruntime.Reader contract.
func (r *IcebergReader) Close() error { return nil }

// Scan implements pipelineruntime.Reader. Builds an Iceberg table
// identifier from (namespace, table) and streams the current
// snapshot's rows as Arrow record batches, yielding one
// pipelineruntime.Row per Arrow row via Arrow's GetOneForMarshal.
func (r *IcebergReader) Scan(ctx context.Context, catalogName, namespace, tableName string) (pipelineruntime.RowStream, error) {
	if catalogName != r.cfg.CatalogName {
		return nil, fmt.Errorf("%w: configured %q, requested %q", ErrUnknownCatalog, r.cfg.CatalogName, catalogName)
	}
	ident := table.Identifier{namespace, tableName}
	tbl, err := r.cat.LoadTable(ctx, ident)
	if err != nil {
		return nil, fmt.Errorf("iceberg reader: load table %v: %w", ident, err)
	}
	scan := tbl.Scan()
	schema, batches, err := scan.ToArrowRecords(ctx)
	if err != nil {
		return nil, fmt.Errorf("iceberg reader: plan scan for %v: %w", ident, err)
	}
	return rowStreamFromBatches(schema, batches), nil
}

// rowStreamFromBatches adapts the apache/arrow record-batch iterator
// into the pipelineruntime.RowStream shape (one Row per Arrow row).
func rowStreamFromBatches(schema *arrow.Schema, batches iter.Seq2[arrow.RecordBatch, error]) pipelineruntime.RowStream {
	return func(yield func(pipelineruntime.Row, error) bool) {
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
	}
}

func batchRowToMap(schema *arrow.Schema, batch arrow.RecordBatch, r int) pipelineruntime.Row {
	n := schema.NumFields()
	row := make(pipelineruntime.Row, n)
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
