// Apache Arrow Flight SQL provider for OpenFoundry.
//
// FlightSQLProvider turns a remote Flight SQL endpoint into a
// callable scan. Any data-plane service can therefore federate
// queries against another Flight SQL service exactly as if its
// result-set were a local table:
//
//	provider, err := queryengine.TryNewFlightSQLProvider(ctx,
//	    "http://flight-sql.internal:50051",
//	    "SELECT id, name FROM customers")
//	if err != nil { return err }
//	rdr, err := provider.Scan(ctx, []int{0, 1}, 10)
//	if err != nil { return err }
//	defer rdr.Release()
//	for rdr.Next() { rec := rdr.RecordBatch(); … rec.Release() }
//	if err := rdr.Err(); err != nil { return err }
//
// # Push-down support
//
//   - Projection is pushed down trivially: only the requested columns
//     are kept once the batches arrive from the remote endpoint.
//   - Limit is pushed down trivially: the local stream stops as soon
//     as `limit` rows have been emitted, slicing the boundary batch
//     as needed.
//   - Filter push-down is intentionally out of scope. Translating
//     arbitrary expression trees into the SQL dialect of an unknown
//     remote engine cannot be done safely in a generic provider.
//     Callers that need filter push-down should embed the predicate
//     directly in the `query` they pass to TryNewFlightSQLProvider.
//
// # Divergence from the Rust crate
//
// The Rust crate exposes `FlightSqlTableProvider` as a DataFusion
// `TableProvider` plus an `ExecutionPlan` (FlightSqlExec). Go has no
// DataFusion equivalent, so we expose the same underlying capability
// — connect, execute, walk endpoints, project, limit — as a plain
// type with a Scan method that returns an `array.RecordReader`. The
// wire-level behaviour, error taxonomy, and push-down policy are
// identical to the Rust port.
package queryengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ─── Errors ────────────────────────────────────────────────────────────

// FlightProviderErrorKind tags FlightProviderError variants. Mirrors
// the Rust `enum FlightProviderError`.
type FlightProviderErrorKind uint8

const (
	// FlightInvalidEndpoint — the supplied endpoint URL could not be
	// parsed or reached.
	FlightInvalidEndpoint FlightProviderErrorKind = iota
	// FlightConnect — the TCP/HTTP-2 connection to the remote Flight
	// server failed.
	FlightConnect
	// FlightServer — the remote Flight SQL server returned an
	// Arrow-level error.
	FlightServer
	// FlightMissingTicket — the Flight server replied with a
	// FlightInfo that did not contain any endpoint (and therefore no
	// ticket) for the query.
	FlightMissingTicket
)

// FlightProviderError is the typed error returned by every
// FlightSQLProvider call. Use errors.As to discriminate.
type FlightProviderError struct {
	Kind     FlightProviderErrorKind
	Endpoint string
	Message  string
	Cause    error
}

func (e *FlightProviderError) Error() string {
	switch e.Kind {
	case FlightInvalidEndpoint:
		return fmt.Sprintf("invalid Flight SQL endpoint %q: %s", e.Endpoint, e.causeOrMsg())
	case FlightConnect:
		return fmt.Sprintf("failed to connect to Flight SQL endpoint %q: %s", e.Endpoint, e.causeOrMsg())
	case FlightServer:
		return "Flight SQL error: " + e.causeOrMsg()
	case FlightMissingTicket:
		return "Flight SQL endpoint returned no ticket for query"
	default:
		return "flight provider error: " + e.causeOrMsg()
	}
}

func (e *FlightProviderError) Unwrap() error { return e.Cause }

func (e *FlightProviderError) causeOrMsg() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "unknown"
}

// IsInvalidEndpoint reports whether err is a FlightInvalidEndpoint.
func IsInvalidEndpoint(err error) bool { return matchFlightKind(err, FlightInvalidEndpoint) }

// IsConnectError reports whether err is a FlightConnect.
func IsConnectError(err error) bool { return matchFlightKind(err, FlightConnect) }

// IsServerError reports whether err is a FlightServer.
func IsServerError(err error) bool { return matchFlightKind(err, FlightServer) }

// IsMissingTicket reports whether err is a FlightMissingTicket.
func IsMissingTicket(err error) bool { return matchFlightKind(err, FlightMissingTicket) }

func matchFlightKind(err error, kind FlightProviderErrorKind) bool {
	var fe *FlightProviderError
	return errors.As(err, &fe) && fe.Kind == kind
}

// ─── Provider ──────────────────────────────────────────────────────────

// FlightSQLProvider resolves its data by issuing a SQL query against
// a remote Apache Arrow Flight SQL endpoint. Mirrors the Rust
// FlightSqlTableProvider.
//
// The schema is supplied up-front (either by the caller via
// NewFlightSQLProvider or by an upfront execute round-trip via
// TryNewFlightSQLProvider) because callers typically need it before
// any I/O takes place. The remote service must produce a result-set
// that is compatible with this schema.
type FlightSQLProvider struct {
	endpoint string
	query    string
	schema   *arrow.Schema
	dialOpts []grpc.DialOption
	alloc    memory.Allocator
}

// FlightProviderOption customises a FlightSQLProvider at construction
// time.
type FlightProviderOption func(*flightProviderOpts)

type flightProviderOpts struct {
	dialOpts []grpc.DialOption
	alloc    memory.Allocator
}

// WithDialOptions overrides the gRPC dial options used to connect
// to the remote Flight SQL endpoint. When unset the provider infers
// transport credentials from the URL scheme: `http://` and bare
// `host:port` use insecure credentials; `https://` uses system TLS.
func WithDialOptions(opts ...grpc.DialOption) FlightProviderOption {
	return func(o *flightProviderOpts) { o.dialOpts = opts }
}

// WithAllocator overrides the Arrow allocator used to materialise
// projected batches. Defaults to memory.DefaultAllocator.
func WithAllocator(alloc memory.Allocator) FlightProviderOption {
	return func(o *flightProviderOpts) { o.alloc = alloc }
}

// TryNewFlightSQLProvider connects to `endpoint`, executes `query`
// once to retrieve the Arrow schema, and returns a provider ready to
// be scanned. Mirrors the Rust try_new constructor.
func TryNewFlightSQLProvider(ctx context.Context, endpoint, query string, opts ...FlightProviderOption) (*FlightSQLProvider, error) {
	options := resolveOpts(endpoint, opts)
	client, err := dialFlight(ctx, endpoint, options.dialOpts)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	info, err := client.Execute(ctx, query)
	if err != nil {
		return nil, &FlightProviderError{Kind: FlightServer, Cause: err}
	}
	schema, err := flight.DeserializeSchema(info.GetSchema(), options.alloc)
	if err != nil {
		return nil, &FlightProviderError{Kind: FlightServer, Cause: err}
	}
	return &FlightSQLProvider{
		endpoint: endpoint,
		query:    query,
		schema:   schema,
		dialOpts: options.dialOpts,
		alloc:    options.alloc,
	}, nil
}

// NewFlightSQLProvider builds a provider with a known schema, skipping
// the upfront network round-trip used by TryNewFlightSQLProvider.
// Useful when the schema is part of a service contract or has already
// been negotiated out-of-band. Mirrors the Rust new constructor.
func NewFlightSQLProvider(endpoint, query string, schema *arrow.Schema, opts ...FlightProviderOption) *FlightSQLProvider {
	options := resolveOpts(endpoint, opts)
	return &FlightSQLProvider{
		endpoint: endpoint,
		query:    query,
		schema:   schema,
		dialOpts: options.dialOpts,
		alloc:    options.alloc,
	}
}

// Endpoint returns the configured Flight SQL endpoint URL.
func (p *FlightSQLProvider) Endpoint() string { return p.endpoint }

// Query returns the SQL statement that will be sent to the remote
// endpoint.
func (p *FlightSQLProvider) Query() string { return p.query }

// Schema returns the result-set schema reported by the remote
// endpoint (or supplied to NewFlightSQLProvider).
func (p *FlightSQLProvider) Schema() *arrow.Schema { return p.schema }

// Scan opens a fresh connection, executes the query, walks every
// endpoint's ticket and returns a RecordReader that streams the
// concatenated batches with the requested projection and limit
// applied locally.
//
// `projection` is a list of column indices (nil = all columns).
// `limit` caps the total number of rows emitted (negative or zero
// = unbounded), slicing the boundary batch as needed.
//
// The caller must Release the returned reader. Mirrors
// `TableProvider::scan` + `FlightSqlExec::execute` in the Rust port.
func (p *FlightSQLProvider) Scan(ctx context.Context, projection []int, limit int) (array.RecordReader, error) {
	projectedSchema, err := projectSchema(p.schema, projection)
	if err != nil {
		return nil, err
	}
	client, err := dialFlight(ctx, p.endpoint, p.dialOpts)
	if err != nil {
		return nil, err
	}
	info, err := client.Execute(ctx, p.query)
	if err != nil {
		client.Close()
		return nil, &FlightProviderError{Kind: FlightServer, Cause: err}
	}
	if len(info.GetEndpoint()) == 0 {
		client.Close()
		return nil, &FlightProviderError{Kind: FlightMissingTicket}
	}
	tickets := make([]*flight.Ticket, 0, len(info.GetEndpoint()))
	for _, ep := range info.GetEndpoint() {
		tk := ep.GetTicket()
		if tk == nil {
			client.Close()
			return nil, &FlightProviderError{Kind: FlightMissingTicket}
		}
		tickets = append(tickets, tk)
	}
	return &flightStreamReader{
		ctx:        ctx,
		client:     client,
		tickets:    tickets,
		projection: append([]int(nil), projection...),
		limit:      limit,
		schema:     projectedSchema,
		alloc:      p.alloc,
	}, nil
}

// ─── Stream reader ─────────────────────────────────────────────────────

// flightStreamReader implements array.RecordReader by walking the
// list of Flight tickets sequentially, applying the requested
// projection + limit on every batch as it arrives. Mirrors the Rust
// `FlightSqlExec::execute` flow (`stream::iter(streams).flatten()` +
// `apply_limit`).
type flightStreamReader struct {
	ctx        context.Context
	client     *flightsql.Client
	tickets    []*flight.Ticket
	tIdx       int
	current    *flight.Reader
	projection []int
	schema     *arrow.Schema
	alloc      memory.Allocator

	limit       int
	enforce     bool
	budget      int
	budgetReady bool

	last    arrow.RecordBatch
	err     error
	closed  bool
	refCnt  int64
	started bool
}

// Schema returns the projected schema. Implements array.RecordReader.
func (r *flightStreamReader) Schema() *arrow.Schema { return r.schema }

// Retain bumps the reference count. Implements array.RecordReader.
func (r *flightStreamReader) Retain() { r.refCnt++ }

// Release drops the reference count and closes the underlying client
// when the count reaches zero. Implements array.RecordReader.
func (r *flightStreamReader) Release() {
	if r.refCnt > 0 {
		r.refCnt--
		if r.refCnt > 0 {
			return
		}
	}
	r.close()
}

// Err returns the terminal error, if any. Implements
// array.RecordReader.
func (r *flightStreamReader) Err() error { return r.err }

// RecordBatch returns the most recently read batch. Implements
// array.RecordReader.
func (r *flightStreamReader) RecordBatch() arrow.RecordBatch { return r.last }

// Record returns the most recently read batch. Deprecated alias for
// RecordBatch retained for backward compatibility with the
// array.RecordReader interface.
func (r *flightStreamReader) Record() arrow.RecordBatch { return r.last }

// Next advances to the next batch. Returns false at end-of-stream
// or on error (use Err to discriminate).
func (r *flightStreamReader) Next() bool {
	if r.closed || r.err != nil {
		return false
	}
	if !r.budgetReady {
		r.enforce = r.limit > 0
		r.budget = r.limit
		r.budgetReady = true
	}
	if r.enforce && r.budget <= 0 {
		return false
	}
	if r.last != nil {
		r.last.Release()
		r.last = nil
	}
	for {
		batch, ok := r.advance()
		if !ok {
			return false
		}
		projected, err := projectBatch(r.alloc, batch, r.projection)
		batch.Release()
		if err != nil {
			r.err = err
			return false
		}
		if r.enforce {
			rows := int(projected.NumRows())
			if rows == 0 {
				projected.Release()
				continue
			}
			if rows > r.budget {
				sliced := projected.NewSlice(0, int64(r.budget))
				projected.Release()
				projected = sliced
				r.budget = 0
			} else {
				r.budget -= rows
			}
		}
		r.last = projected
		return true
	}
}

// advance pulls the next raw batch from the underlying flight
// readers, opening fresh ones as tickets are consumed.
func (r *flightStreamReader) advance() (arrow.RecordBatch, bool) {
	for {
		if r.current == nil {
			if r.tIdx >= len(r.tickets) {
				return nil, false
			}
			rdr, err := r.client.DoGet(r.ctx, r.tickets[r.tIdx])
			if err != nil {
				r.err = &FlightProviderError{Kind: FlightServer, Cause: err}
				return nil, false
			}
			r.tIdx++
			r.current = rdr
			r.started = true
		}
		if r.current.Next() {
			rec := r.current.RecordBatch()
			rec.Retain()
			return rec, true
		}
		if cerr := r.current.Err(); cerr != nil && !errors.Is(cerr, io.EOF) {
			r.err = &FlightProviderError{Kind: FlightServer, Cause: cerr}
			r.current.Release()
			r.current = nil
			return nil, false
		}
		r.current.Release()
		r.current = nil
	}
}

func (r *flightStreamReader) close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.last != nil {
		r.last.Release()
		r.last = nil
	}
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.client != nil {
		_ = r.client.Close()
		r.client = nil
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────

// dialFlight is the Go analogue of `connect` in the Rust port.
// Strips the URL scheme so gRPC sees a `host:port` target, then
// dials the remote endpoint with caller-supplied (or scheme-inferred)
// credentials.
func dialFlight(ctx context.Context, endpoint string, dialOpts []grpc.DialOption) (*flightsql.Client, error) {
	target := normaliseFlightEndpoint(endpoint)
	if target == "" {
		return nil, &FlightProviderError{Kind: FlightInvalidEndpoint, Endpoint: endpoint, Message: "empty endpoint"}
	}
	if dialOpts == nil {
		dialOpts = []grpc.DialOption{inferTransportCredentials(endpoint)}
	}
	client, err := flightsql.NewClientCtx(ctx, target, nil, nil, dialOpts...)
	if err != nil {
		return nil, &FlightProviderError{Kind: FlightConnect, Endpoint: endpoint, Cause: err}
	}
	return client, nil
}

// normaliseFlightEndpoint strips well-known schemes + trailing slash
// so the gRPC dialer sees a `host:port` target. Mirrors the same
// helper in services/sql-bi-gateway-service/internal/flightsql.
func normaliseFlightEndpoint(endpoint string) string {
	t := strings.TrimSpace(endpoint)
	for _, prefix := range []string{"http://", "https://", "grpc://", "grpc+tcp://", "grpc+tls://"} {
		t = strings.TrimPrefix(t, prefix)
	}
	return strings.TrimRight(t, "/")
}

// inferTransportCredentials picks insecure credentials for plain
// `http://` (and bare `host:port`) and system TLS for `https://` /
// `grpc+tls://`.
func inferTransportCredentials(endpoint string) grpc.DialOption {
	low := strings.ToLower(strings.TrimSpace(endpoint))
	if strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "grpc+tls://") {
		return grpc.WithTransportCredentials(credentials.NewTLS(nil))
	}
	return grpc.WithTransportCredentials(insecure.NewCredentials())
}

// projectSchema returns the projection of `schema` keeping only the
// columns whose indices appear in `cols`. Passing nil returns the
// schema unchanged. Mirrors `datafusion::common::project_schema`.
func projectSchema(schema *arrow.Schema, cols []int) (*arrow.Schema, error) {
	if cols == nil {
		return schema, nil
	}
	fields := make([]arrow.Field, len(cols))
	for i, c := range cols {
		if c < 0 || c >= len(schema.Fields()) {
			return nil, &FlightProviderError{
				Kind:    FlightServer,
				Message: fmt.Sprintf("projection index %d out of range for schema with %d columns", c, len(schema.Fields())),
			}
		}
		fields[i] = schema.Field(c)
	}
	md := schema.Metadata()
	return arrow.NewSchema(fields, &md), nil
}

// projectBatch slices a batch down to the requested column indices.
// Returns a freshly-retained batch the caller must Release. Passing
// a nil projection returns the batch unchanged (with an extra Retain
// so the caller may always Release).
func projectBatch(alloc memory.Allocator, batch arrow.RecordBatch, cols []int) (arrow.RecordBatch, error) {
	if cols == nil {
		batch.Retain()
		return batch, nil
	}
	projectedSchema, err := projectSchema(batch.Schema(), cols)
	if err != nil {
		return nil, err
	}
	arrays := make([]arrow.Array, len(cols))
	for i, c := range cols {
		if c < 0 || c >= int(batch.NumCols()) {
			return nil, &FlightProviderError{
				Kind:    FlightServer,
				Message: fmt.Sprintf("projection index %d out of range for batch with %d columns", c, batch.NumCols()),
			}
		}
		arrays[i] = batch.Column(c)
		arrays[i].Retain()
	}
	rec := array.NewRecordBatch(projectedSchema, arrays, batch.NumRows())
	for _, a := range arrays {
		a.Release()
	}
	_ = alloc // reserved for future builder-based projections
	return rec, nil
}

// resolveOpts merges caller-supplied options with the defaults.
func resolveOpts(endpoint string, opts []FlightProviderOption) flightProviderOpts {
	resolved := flightProviderOpts{}
	for _, o := range opts {
		o(&resolved)
	}
	if resolved.alloc == nil {
		resolved.alloc = memory.DefaultAllocator
	}
	if resolved.dialOpts == nil {
		resolved.dialOpts = []grpc.DialOption{inferTransportCredentials(endpoint)}
	}
	return resolved
}
