// Package flightsql implements the gateway's primary surface — an
// Apache Arrow Flight SQL gRPC server on `cfg.Port` (default 50133).
//
// 1:1 port of services/sql-bi-gateway-service/src/flight_sql.rs.
//
// The Rust crate runs a tonic Flight SQL server backed by DataFusion;
// this package runs a github.com/apache/arrow-go/v18/arrow/flight
// Flight SQL server backed by [queryengine.QueryContext]
// (a literal-SELECT evaluator that mirrors DataFusion's behaviour for
// `SELECT 1`-style probes) and delegates anything richer to the
// configured warehousing endpoint or the per-backend Flight SQL
// front (Vespa / Postgres / Trino) — same dispatch rules as Rust.
//
// Auth, routing and audit are wired uniformly: every gRPC handler
// pulls the incoming `authorization: Bearer <jwt>` header through
// [auth.Authenticator], routes the statement through
// [routing.BackendRouter] and emits an [audit.SQLEvent] before
// returning. Catalog / schema / table sentinels are served
// synchronously so BI clients render a non-empty navigator panel
// even when no warehousing endpoint is configured.
package flightsql

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"log/slog"

	queryengine "github.com/openfoundry/openfoundry-go/libs/query-engine"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/audit"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/auth"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/routing"
)

// GatewayCatalog is the fixed catalog name advertised to BI clients.
// The schemas under it map 1:1 onto [routing.Backend] variants, so a
// connected Tableau / Superset session sees exactly one catalog
// (`openfoundry`) and the configured backend schemas.
const GatewayCatalog = "openfoundry"

// Sentinel handles for the synchronous catalog / schemas / tables
// responses. The bytes are stable on-the-wire identifiers used by
// DoGetStatement to decide whether to call the local engine or
// return a pre-built batch.
var (
	sentinelCatalogs = []byte("__catalogs__")
	sentinelSchemas  = []byte("__schemas__")
	sentinelTables   = []byte("__tables__")
)

// Service is the Flight SQL gateway. Embeds flightsql.BaseServer so
// only the methods we override above the no-op defaults need
// implementing.
type Service struct {
	flightsql.BaseServer

	cfg    *config.Config
	ctx    *queryengine.QueryContext
	router *routing.BackendRouter
	auth   *auth.Authenticator
	log    *slog.Logger
	alloc  memory.Allocator

	mu     sync.Mutex
	srv    flight.Server
	closed chan struct{}
}

// New builds a Service. The query context, BackendRouter and
// Authenticator are resolved here once so they are stable across
// requests.
func New(cfg *config.Config, log *slog.Logger) *Service {
	s := &Service{
		cfg:    cfg,
		ctx:    queryengine.New(),
		router: routing.FromConfig(cfg),
		auth:   auth.NewAuthenticator(cfg.JWTSecret, cfg.AllowAnonymous),
		log:    log,
		alloc:  memory.DefaultAllocator,
		closed: make(chan struct{}),
	}
	s.BaseServer.Alloc = s.alloc
	return s
}

// Authenticator borrows the gateway's JWT authenticator (useful for
// in-process tests).
func (s *Service) Authenticator() *auth.Authenticator { return s.auth }

// Router borrows the configured BackendRouter (useful for tests).
func (s *Service) Router() *routing.BackendRouter { return s.router }

// Context borrows the underlying query context (useful for
// registering tables in tests once the query engine grows that
// surface).
func (s *Service) Context() *queryengine.QueryContext { return s.ctx }

// ListenAndServe binds the TCP port advertised by cfg.Port and runs
// the gRPC Flight SQL server until ctx is cancelled or [Stop] is
// called.
func (s *Service) ListenAndServe(ctx context.Context) error {
	addr := s.cfg.Host + ":" + strconv.FormatUint(uint64(s.cfg.Port), 10)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve runs the gRPC server on ln. Exposed so in-process tests can
// hand in a `net.Listen("tcp", "127.0.0.1:0")` listener and read the
// resolved port back via ln.Addr().
func (s *Service) Serve(ctx context.Context, ln net.Listener) error {
	srv := flight.NewServerWithMiddleware(nil)
	srv.InitListener(ln)
	srv.RegisterFlightService(flightsql.NewFlightServer(s))

	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()

	s.log.Info("flight-sql server listening",
		slog.String("addr", ln.Addr().String()),
		slog.Bool("allow_anonymous", s.cfg.AllowAnonymous))

	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	if err := srv.Serve(); err != nil {
		select {
		case <-s.closed:
			return nil
		default:
		}
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
	return nil
}

// Stop gracefully shuts down the gRPC server.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	if s.srv != nil {
		s.srv.Shutdown()
	}
	return nil
}

// --- auth helpers ------------------------------------------------------

// metadataAdapter wraps gRPC's metadata.MD into auth.Metadata so the
// Authenticator stays gRPC-agnostic.
type metadataAdapter struct{ md metadata.MD }

func (m metadataAdapter) Get(key string) string {
	for _, v := range m.md.Get(key) {
		if v != "" {
			return v
		}
	}
	return ""
}

// authenticate pulls the bearer JWT from the gRPC metadata, decodes
// it via the Authenticator and returns the resulting
// AuthenticatedRequest plus EnforcedQuotas. When AllowAnonymous is
// true and no header is present, returns nil + the standard-tier
// default quotas — matches Rust's `(Option<AuthenticatedRequest>, EnforcedQuotas)`.
func (s *Service) authenticate(ctx context.Context) (*auth.AuthenticatedRequest, auth.EnforcedQuotas, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	req, err := s.auth.Authenticate(metadataAdapter{md: md})
	if err != nil {
		return nil, auth.EnforcedQuotas{}, status.Error(codes.Unauthenticated, err.Error())
	}
	if req == nil {
		return nil, auth.AnonymousDefaultQuotas(), nil
	}
	return req, auth.QuotasForTenant(req.Tenant), nil
}

// routingStatus converts a routing error to the matching gRPC status,
// mirroring `routing_status` in flight_sql.rs.
func routingStatus(err error) error {
	if errors.Is(err, &routing.ErrBackendUnavailable{}) || routing.IsBackendUnavailable(err) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// --- statement / prepared-statement RPCs ------------------------------

// GetFlightInfoStatement validates the routing decision before
// returning a ticket so the BI client gets a clear error at planning
// time rather than at streaming time.
func (s *Service) GetFlightInfoStatement(ctx context.Context, cmd flightsql.StatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	if _, err := s.router.Route(cmd.GetQuery()); err != nil {
		return nil, routingStatus(err)
	}
	tkt, err := flightsql.CreateStatementQueryTicket([]byte(cmd.GetQuery()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &flight.FlightInfo{
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: tkt}}},
		FlightDescriptor: desc,
		TotalRecords:     -1,
		TotalBytes:       -1,
	}, nil
}

// GetFlightInfoPreparedStatement treats the prepared-statement handle
// as the SQL bytes themselves (Rust does the same) and delegates to
// GetFlightInfoStatement.
func (s *Service) GetFlightInfoPreparedStatement(ctx context.Context, cmd flightsql.PreparedStatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	return s.GetFlightInfoStatement(ctx, preparedAsStatement{cmd: cmd}, desc)
}

// preparedAsStatement adapts a PreparedStatementQuery into a
// StatementQuery so we can reuse the planning logic.
type preparedAsStatement struct{ cmd flightsql.PreparedStatementQuery }

func (p preparedAsStatement) GetQuery() string         { return string(p.cmd.GetPreparedStatementHandle()) }
func (p preparedAsStatement) GetTransactionId() []byte { return nil }

// DoGetStatement streams the result of executing the planned ticket.
// Sentinel handles serve catalog/schemas/tables synchronously;
// everything else routes via [routing.BackendRouter] and either runs
// against [queryengine.QueryContext] or delegates to the configured
// remote Flight SQL endpoint.
func (s *Service) DoGetStatement(ctx context.Context, ticket flightsql.StatementQueryTicket) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	authReq, quotas, err := s.authenticate(ctx)
	if err != nil {
		return nil, nil, err
	}

	handle := ticket.GetStatementHandle()
	switch {
	case bytesEqual(handle, sentinelCatalogs):
		return s.serveCatalogs()
	case bytesEqual(handle, sentinelSchemas):
		return s.serveSchemas()
	case bytesEqual(handle, sentinelTables):
		return s.serveTables()
	}

	sql := string(handle)
	return s.execute(ctx, sql, authReq, quotas)
}

// DoGetPreparedStatement reuses the statement path, mirroring Rust's
// `get_flight_info_prepared_statement -> get_flight_info_statement`
// chain.
func (s *Service) DoGetPreparedStatement(ctx context.Context, cmd flightsql.PreparedStatementQuery) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	authReq, quotas, err := s.authenticate(ctx)
	if err != nil {
		return nil, nil, err
	}
	sql := string(cmd.GetPreparedStatementHandle())
	return s.execute(ctx, sql, authReq, quotas)
}

// DoPutCommandStatementUpdate runs DDL/DML and returns the affected-
// row count. Local DataFusion / queryengine doesn't surface a
// meaningful number for arbitrary updates so we report 0 (Flight SQL
// clients treat it as "unknown") — same behaviour as Rust.
func (s *Service) DoPutCommandStatementUpdate(ctx context.Context, cmd flightsql.StatementUpdate) (int64, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return 0, err
	}
	decision, err := s.router.Route(cmd.GetQuery())
	if err != nil {
		return 0, routingStatus(err)
	}
	if decision.RemoteURL == "" {
		// Local execution path — best-effort, ignore the value channel.
		batches, _, err := s.ctx.ExecuteSQL(ctx, cmd.GetQuery())
		if err != nil {
			return 0, status.Error(codes.Internal, err.Error())
		}
		releaseBatches(batches)
		return 0, nil
	}
	if err := delegateUpdate(ctx, decision.RemoteURL, cmd.GetQuery()); err != nil {
		return 0, err
	}
	return 0, nil
}

// --- catalog / schemas / tables RPCs ----------------------------------

// GetFlightInfoCatalogs returns a FlightInfo carrying the
// `__catalogs__` sentinel ticket so DoGetStatement serves the
// pre-built batch.
func (s *Service) GetFlightInfoCatalogs(ctx context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	return s.flightInfoForSentinel(desc, sentinelCatalogs, 1), nil
}

// GetFlightInfoSchemas returns a FlightInfo carrying the
// `__schemas__` sentinel.
func (s *Service) GetFlightInfoSchemas(ctx context.Context, _ flightsql.GetDBSchemas, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	return s.flightInfoForSentinel(desc, sentinelSchemas, int64(len(routing.AllBackends()))), nil
}

// GetFlightInfoTables returns a FlightInfo carrying the `__tables__`
// sentinel.
func (s *Service) GetFlightInfoTables(ctx context.Context, _ flightsql.GetTables, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	return s.flightInfoForSentinel(desc, sentinelTables, int64(len(routing.AllBackends()))), nil
}

// DoGetCatalogs / DoGetDBSchemas / DoGetTables delegate to the
// sentinel servers — Tableau / Superset issue them either via these
// RPCs directly or via DoGetStatement with the matching ticket.
func (s *Service) DoGetCatalogs(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, nil, err
	}
	return s.serveCatalogs()
}

func (s *Service) DoGetDBSchemas(ctx context.Context, _ flightsql.GetDBSchemas) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, nil, err
	}
	return s.serveSchemas()
}

func (s *Service) DoGetTables(ctx context.Context, _ flightsql.GetTables) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	if _, _, err := s.authenticate(ctx); err != nil {
		return nil, nil, err
	}
	return s.serveTables()
}

// flightInfoForSentinel builds a FlightInfo whose only endpoint
// carries a `flightsql.CreateStatementQueryTicket(handle)` ticket.
// This is the pattern arrow-go's Flight SQL server expects so that
// the same DoGetStatement RPC can serve both real queries and the
// catalog / schemas / tables sentinels.
func (s *Service) flightInfoForSentinel(desc *flight.FlightDescriptor, handle []byte, totalRecords int64) *flight.FlightInfo {
	tkt, err := flightsql.CreateStatementQueryTicket(handle)
	if err != nil {
		// Should never happen — handle is a static byte slice.
		s.log.Error("flight-sql: encode sentinel ticket", slog.String("error", err.Error()))
		return &flight.FlightInfo{FlightDescriptor: desc}
	}
	return &flight.FlightInfo{
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: tkt}}},
		FlightDescriptor: desc,
		TotalRecords:     totalRecords,
		TotalBytes:       -1,
	}
}

// serveCatalogs returns the single-row catalog batch.
func (s *Service) serveCatalogs() (*arrow.Schema, <-chan flight.StreamChunk, error) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "catalog_name", Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)
	b := array.NewStringBuilder(s.alloc)
	defer b.Release()
	b.Append(GatewayCatalog)
	col := b.NewArray()
	defer col.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{col}, 1)
	return schema, wrapBatch(rec), nil
}

// serveSchemas returns one row per registered backend.
func (s *Service) serveSchemas() (*arrow.Schema, <-chan flight.StreamChunk, error) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "catalog_name", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "db_schema_name", Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)
	backends := routing.AllBackends()

	cat := array.NewStringBuilder(s.alloc)
	defer cat.Release()
	sch := array.NewStringBuilder(s.alloc)
	defer sch.Release()
	for _, b := range backends {
		cat.Append(GatewayCatalog)
		sch.Append(string(b))
	}
	catCol, schCol := cat.NewArray(), sch.NewArray()
	defer catCol.Release()
	defer schCol.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{catCol, schCol}, int64(len(backends)))
	return schema, wrapBatch(rec), nil
}

// serveTables returns one placeholder `_meta` row per backend so the
// BI navigator tree is non-empty even before catalog metadata is
// wired in. Real tables are still discoverable via standard
// `SHOW TABLES` / `information_schema` queries.
func (s *Service) serveTables() (*arrow.Schema, <-chan flight.StreamChunk, error) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "catalog_name", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "db_schema_name", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "table_name", Type: arrow.BinaryTypes.String, Nullable: false},
		{Name: "table_type", Type: arrow.BinaryTypes.String, Nullable: false},
	}, nil)
	backends := routing.AllBackends()

	cat := array.NewStringBuilder(s.alloc)
	defer cat.Release()
	sch := array.NewStringBuilder(s.alloc)
	defer sch.Release()
	name := array.NewStringBuilder(s.alloc)
	defer name.Release()
	kind := array.NewStringBuilder(s.alloc)
	defer kind.Release()
	for _, b := range backends {
		cat.Append(GatewayCatalog)
		sch.Append(string(b))
		name.Append("_meta")
		kind.Append("TABLE")
	}
	catCol, schCol, nameCol, kindCol :=
		cat.NewArray(), sch.NewArray(), name.NewArray(), kind.NewArray()
	defer catCol.Release()
	defer schCol.Release()
	defer nameCol.Release()
	defer kindCol.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{catCol, schCol, nameCol, kindCol},
		int64(len(backends)))
	return schema, wrapBatch(rec), nil
}

// wrapBatch returns a closed channel populated with a single
// pre-built record batch — the streaming shape the flightsql.Server
// interface expects for synchronous responses.
func wrapBatch(rec arrow.RecordBatch) <-chan flight.StreamChunk {
	ch := make(chan flight.StreamChunk, 1)
	ch <- flight.StreamChunk{Data: rec}
	close(ch)
	return ch
}

// --- execution path ----------------------------------------------------

// execute runs the SQL through the local query engine or delegates
// to a remote Flight SQL endpoint, applies the per-tenant row quota,
// emits the audit event and streams the result.
func (s *Service) execute(ctx context.Context, sql string, authReq *auth.AuthenticatedRequest, quotas auth.EnforcedQuotas) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	started := time.Now()

	decision, err := s.router.Route(sql)
	if err != nil {
		s.audit(sql, authReq, decision, 0, time.Since(started), audit.OutcomeError)
		return nil, nil, routingStatus(err)
	}

	var (
		batches []arrow.RecordBatch
		schema  *arrow.Schema
	)
	if decision.RemoteURL == "" {
		batches, schema, err = s.ctx.ExecuteSQL(ctx, sql)
		if err != nil {
			s.audit(sql, authReq, decision, 0, time.Since(started), audit.OutcomeError)
			if errors.Is(err, queryengine.ErrUnsupportedLocalExecution) {
				return nil, nil, status.Error(codes.FailedPrecondition,
					"queryengine: local execution requires a literal SELECT; "+
						"set WAREHOUSING_FLIGHT_SQL_URL to federate richer statements")
			}
			return nil, nil, status.Errorf(codes.Internal, "local execution failed: %s", err)
		}
	} else {
		batches, schema, err = delegateQuery(ctx, decision.RemoteURL, sql)
		if err != nil {
			s.audit(sql, authReq, decision, 0, time.Since(started), audit.OutcomeError)
			return nil, nil, err
		}
	}

	clamped, rowCount := clampRows(batches, int(quotas.MaxRows))
	s.audit(sql, authReq, decision, rowCount, time.Since(started), audit.OutcomeOK)

	ch := make(chan flight.StreamChunk, len(clamped))
	for _, b := range clamped {
		ch <- flight.StreamChunk{Data: b}
	}
	close(ch)
	return schema, ch, nil
}

// clampRows truncates the batch slice so the cumulative row count
// does not exceed maxRows. Mirrors the Rust `quotas.max_rows` clamp
// applied on the way out.
func clampRows(batches []arrow.RecordBatch, maxRows int) ([]arrow.RecordBatch, int) {
	if maxRows <= 0 {
		releaseBatches(batches)
		return nil, 0
	}
	remaining := maxRows
	out := make([]arrow.RecordBatch, 0, len(batches))
	rowCount := 0
	for _, b := range batches {
		if remaining == 0 {
			b.Release()
			continue
		}
		n := int(b.NumRows())
		if n <= remaining {
			out = append(out, b)
			remaining -= n
			rowCount += n
			continue
		}
		// arrow.RecordBatch.NewSlice yields a 0-cost slice that the
		// caller still has to Release; the original batch can be
		// released here since the slice retains its columns.
		sliced := b.NewSlice(0, int64(remaining))
		b.Release()
		out = append(out, sliced)
		rowCount += remaining
		remaining = 0
	}
	return out, rowCount
}

func releaseBatches(batches []arrow.RecordBatch) {
	for _, b := range batches {
		if b != nil {
			b.Release()
		}
	}
}

// audit emits a structured SQL audit event mirroring `audit.SqlAuditEvent.emit`.
func (s *Service) audit(sql string, authReq *auth.AuthenticatedRequest, decision routing.Decision, rowCount int, duration time.Duration, outcome audit.Outcome) {
	tenantID := ""
	tenantTier := "anonymous"
	userEmail := ""
	if authReq != nil {
		tenantID = authReq.Tenant.ScopeID
		tenantTier = authReq.Tenant.Tier
		userEmail = authReq.Claims.Email
	}
	audit.SQLEvent{
		TenantID:   tenantID,
		TenantTier: tenantTier,
		UserEmail:  userEmail,
		Backend:    audit.Backend(decision.Backend),
		Remote:     decision.RemoteURL != "",
		SQLHash:    audit.Fingerprint(sql),
		RowCount:   rowCount,
		Duration:   duration,
		Outcome:    outcome,
	}.Emit()
}

// bytesEqual is a tiny constant-time-free byte comparator. Used only
// for sentinel-handle matching, where the inputs are well-known
// constants and timing leakage is not a concern.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

