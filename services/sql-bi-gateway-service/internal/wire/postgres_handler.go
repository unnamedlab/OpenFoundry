// Package wire implements the PostgreSQL wire protocol v3 endpoint of
// sql-bi-gateway-service.
//
// This is the listener Tableau, Power BI, and `psql` connect to when
// the BI driver speaks Postgres rather than Arrow Flight SQL. Only the
// Simple Query subprotocol is supported: a client sends `Query`, the
// gateway folds the statement through libs/query-engine (literal
// SELECTs only — see [`queryengine.ErrUnsupportedLocalExecution`]) and
// streams back one `RowDescription` + N `DataRow` + `CommandComplete`.
//
// Extended Query (Parse/Bind/Execute), COPY, transactions, prepared
// statements, and SSL/SCRAM are intentionally out of scope. The
// gateway answers `SSLRequest` with the single-byte `N` (deny) so the
// stock libpq retry-without-TLS path succeeds, and authentication is
// always `AuthenticationOk` — the upstream identity-federation surface
// gates ingress before traffic reaches this socket.
package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/jackc/pgx/v5/pgproto3"

	queryengine "github.com/openfoundry/openfoundry-go/libs/query-engine"
)

// Postgres OIDs for the Arrow types the literal evaluator can produce.
// Kept inline rather than imported from a pg_type catalog because the
// gateway only emits this fixed set.
const (
	oidBool    uint32 = 16
	oidInt8    uint32 = 20
	oidInt4    uint32 = 23
	oidText    uint32 = 25
	oidFloat8  uint32 = 701
	oidVarchar uint32 = 1043
)

// Engine is the subset of [queryengine.QueryContext] that the wire
// handler needs. Declared as an interface so tests can swap in a stub
// that returns canned record batches without touching the literal
// evaluator.
type Engine interface {
	ExecuteSQL(ctx context.Context, sql string) ([]arrow.RecordBatch, *arrow.Schema, error)
}

// Server hosts the Postgres wire listener.
type Server struct {
	addr   string
	log    *slog.Logger
	engine Engine

	mu       sync.Mutex
	listener net.Listener
	closed   bool
}

// New constructs a Server. If engine is nil the default literal
// evaluator from libs/query-engine is used.
func New(addr string, log *slog.Logger, engine Engine) *Server {
	if engine == nil {
		engine = queryengine.New()
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{addr: addr, log: log, engine: engine}
}

// Addr returns the bound address once the listener is up. Empty
// before [Server.Listen] succeeds.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Listen binds the TCP socket without serving. Useful for tests that
// want a deterministic ephemeral port via "127.0.0.1:0".
func (s *Server) Listen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("wire: server already stopped")
	}
	if s.listener != nil {
		return nil
	}
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("wire: listen %s: %w", s.addr, err)
	}
	s.listener = l
	return nil
}

// Serve accepts connections until the listener is closed. Always
// returns a non-nil error; the canonical clean-shutdown error is
// [net.ErrClosed].
func (s *Server) Serve(ctx context.Context) error {
	if err := s.Listen(); err != nil {
		return err
	}
	s.mu.Lock()
	l := s.listener
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := l.Accept()
		if err != nil {
			wg.Wait()
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			return fmt.Errorf("wire: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

// Stop closes the listener; in-flight connections drain naturally
// when their client closes the socket.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.listener = nil
	return err
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	be := pgproto3.NewBackend(conn, conn)

	if err := s.handshake(be, conn); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			s.log.Debug("wire: handshake failed", slog.String("error", err.Error()))
		}
		return
	}

	for {
		msg, err := be.Receive()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				s.log.Debug("wire: receive failed", slog.String("error", err.Error()))
			}
			return
		}
		switch m := msg.(type) {
		case *pgproto3.Query:
			if err := s.handleQuery(ctx, be, m.String); err != nil {
				s.log.Debug("wire: query handling failed", slog.String("error", err.Error()))
				return
			}
		case *pgproto3.Terminate:
			return
		case *pgproto3.Sync:
			be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err := be.Flush(); err != nil {
				return
			}
		default:
			s.sendUnsupported(be, fmt.Sprintf("unsupported frontend message %T", m))
			be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err := be.Flush(); err != nil {
				return
			}
		}
	}
}

// handshake covers SSLRequest → 'N' deny, StartupMessage →
// AuthenticationOk + ParameterStatus + BackendKeyData + ReadyForQuery.
// Returning a non-nil error aborts the connection.
func (s *Server) handshake(be *pgproto3.Backend, conn net.Conn) error {
	for attempt := 0; attempt < 2; attempt++ {
		startup, err := be.ReceiveStartupMessage()
		if err != nil {
			return fmt.Errorf("receive startup: %w", err)
		}
		switch startup.(type) {
		case *pgproto3.SSLRequest:
			if _, err := conn.Write([]byte{'N'}); err != nil {
				return fmt.Errorf("ssl deny: %w", err)
			}
			continue
		case *pgproto3.GSSEncRequest:
			if _, err := conn.Write([]byte{'N'}); err != nil {
				return fmt.Errorf("gss deny: %w", err)
			}
			continue
		case *pgproto3.CancelRequest:
			return errors.New("cancel request not supported")
		case *pgproto3.StartupMessage:
			be.Send(&pgproto3.AuthenticationOk{})
			be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "14.0 (openfoundry-bi)"})
			be.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
			be.Send(&pgproto3.ParameterStatus{Name: "DateStyle", Value: "ISO, MDY"})
			be.Send(&pgproto3.ParameterStatus{Name: "integer_datetimes", Value: "on"})
			be.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
			be.Send(&pgproto3.ParameterStatus{Name: "TimeZone", Value: "UTC"})
			be.Send(&pgproto3.BackendKeyData{ProcessID: 0, SecretKey: []byte{0, 0, 0, 0}})
			be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			return be.Flush()
		default:
			return fmt.Errorf("unexpected startup frame %T", startup)
		}
	}
	return errors.New("repeated transport negotiation")
}

func (s *Server) handleQuery(ctx context.Context, be *pgproto3.Backend, sql string) error {
	if isEmptyOrCommentOnly(sql) {
		be.Send(&pgproto3.EmptyQueryResponse{})
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return be.Flush()
	}
	batches, schema, err := s.engine.ExecuteSQL(ctx, sql)
	if err != nil {
		s.sendError(be, "FEATURE_NOT_SUPPORTED", "0A000", err.Error())
		be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return be.Flush()
	}
	defer func() {
		for _, b := range batches {
			b.Release()
		}
	}()

	be.Send(buildRowDescription(schema))

	var rows int64
	for _, batch := range batches {
		n := batch.NumRows()
		for i := int64(0); i < n; i++ {
			be.Send(buildDataRow(batch, i))
			rows++
		}
	}
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte(commandTag(sql, rows))})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return be.Flush()
}

func (s *Server) sendError(be *pgproto3.Backend, severity, code, msg string) {
	be.Send(&pgproto3.ErrorResponse{
		Severity:            severity,
		SeverityUnlocalized: severity,
		Code:                code,
		Message:             msg,
	})
}

func (s *Server) sendUnsupported(be *pgproto3.Backend, msg string) {
	s.sendError(be, "ERROR", "0A000", msg)
}

func buildRowDescription(schema *arrow.Schema) *pgproto3.RowDescription {
	fields := schema.Fields()
	out := make([]pgproto3.FieldDescription, len(fields))
	for i, f := range fields {
		oid, size := pgTypeFor(f.Type)
		out[i] = pgproto3.FieldDescription{
			Name:                 []byte(columnLabel(f.Name, i)),
			TableOID:             0,
			TableAttributeNumber: 0,
			DataTypeOID:          oid,
			DataTypeSize:         size,
			TypeModifier:         -1,
			Format:               0, // text
		}
	}
	return &pgproto3.RowDescription{Fields: out}
}

// columnLabel mirrors Postgres' behaviour of emitting "?column?" for
// SELECT expressions with no explicit alias. libs/query-engine names
// anonymous columns colN for Flight SQL parity; we relabel here so
// `SELECT 1` shows up as the canonical libpq column name BI tools
// expect.
func columnLabel(name string, idx int) string {
	if len(name) > 3 && name[:3] == "col" {
		if _, err := strconv.Atoi(name[3:]); err == nil {
			return "?column?"
		}
	}
	if name == "" {
		return fmt.Sprintf("column%d", idx+1)
	}
	return name
}

func pgTypeFor(t arrow.DataType) (uint32, int16) {
	switch t.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.UINT8, arrow.UINT16:
		return oidInt4, 4
	case arrow.INT64, arrow.UINT32, arrow.UINT64:
		return oidInt8, 8
	case arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		return oidFloat8, 8
	case arrow.BOOL:
		return oidBool, 1
	case arrow.STRING, arrow.LARGE_STRING:
		return oidText, -1
	case arrow.BINARY, arrow.LARGE_BINARY:
		return oidVarchar, -1
	case arrow.NULL:
		return oidText, -1
	default:
		return oidText, -1
	}
}

func buildDataRow(batch arrow.RecordBatch, row int64) *pgproto3.DataRow {
	cols := batch.NumCols()
	values := make([][]byte, cols)
	for c := int64(0); c < cols; c++ {
		values[c] = encodeText(batch.Column(int(c)), row)
	}
	return &pgproto3.DataRow{Values: values}
}

// encodeText renders a single Arrow cell as the Postgres text wire
// representation. Returning a nil slice signals SQL NULL, matching
// `DataRow`'s length=-1 encoding.
func encodeText(col arrow.Array, row int64) []byte {
	if col.IsNull(int(row)) {
		return nil
	}
	switch a := col.(type) {
	case *array.Int8:
		return []byte(strconv.FormatInt(int64(a.Value(int(row))), 10))
	case *array.Int16:
		return []byte(strconv.FormatInt(int64(a.Value(int(row))), 10))
	case *array.Int32:
		return []byte(strconv.FormatInt(int64(a.Value(int(row))), 10))
	case *array.Int64:
		return []byte(strconv.FormatInt(a.Value(int(row)), 10))
	case *array.Uint8:
		return []byte(strconv.FormatUint(uint64(a.Value(int(row))), 10))
	case *array.Uint16:
		return []byte(strconv.FormatUint(uint64(a.Value(int(row))), 10))
	case *array.Uint32:
		return []byte(strconv.FormatUint(uint64(a.Value(int(row))), 10))
	case *array.Uint64:
		return []byte(strconv.FormatUint(a.Value(int(row)), 10))
	case *array.Float32:
		return []byte(strconv.FormatFloat(float64(a.Value(int(row))), 'g', -1, 32))
	case *array.Float64:
		return []byte(strconv.FormatFloat(a.Value(int(row)), 'g', -1, 64))
	case *array.Boolean:
		if a.Value(int(row)) {
			return []byte{'t'}
		}
		return []byte{'f'}
	case *array.String:
		return []byte(a.Value(int(row)))
	case *array.LargeString:
		return []byte(a.Value(int(row)))
	case *array.Binary:
		return a.Value(int(row))
	case *array.Null:
		return nil
	default:
		return []byte(fmt.Sprintf("%v", col.ValueStr(int(row))))
	}
}

// commandTag mirrors libpq's CommandComplete tags. For BI clients the
// only one that matters for `SELECT 1` is `SELECT <rowcount>`; any
// non-SELECT path that reaches this code returns "SELECT 0" because
// the literal evaluator never executes DML.
func commandTag(sql string, rows int64) string {
	_ = sql
	return "SELECT " + strconv.FormatInt(rows, 10)
}

// isEmptyOrCommentOnly mirrors Postgres' EmptyQuery handling: a SQL
// string that contains only whitespace and -- line comments yields
// EmptyQueryResponse rather than an error. libpq's `Ping` sends
// `-- ping`, which is the canonical case.
func isEmptyOrCommentOnly(sql string) bool {
	i := 0
	for i < len(sql) {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == ';':
			i++
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			j := i + 2
			for j < len(sql) && sql[j] != '\n' {
				j++
			}
			i = j
		default:
			return false
		}
	}
	return true
}
