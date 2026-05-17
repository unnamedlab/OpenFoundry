package wire

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// startTestServer boots the wire server on an ephemeral port and
// returns the bound `host:port` plus a shutdown closure. The server
// is plumbed against the default literal-evaluator query context so
// SELECT 1 round-trips without any other infrastructure.
func startTestServer(t *testing.T) (string, func()) {
	t.Helper()
	srv := New("127.0.0.1:0", nil, nil)
	if err := srv.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := srv.Addr()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Logf("serve returned: %v", err)
		}
	}()
	return addr, func() {
		cancel()
		_ = srv.Stop()
		<-done
	}
}

func openDB(t *testing.T, addr string) *sql.DB {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	dsn := fmt.Sprintf("host=%s port=%s user=dummy dbname=of sslmode=disable default_query_exec_mode=simple_protocol", host, port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)

	deadline := time.Now().Add(5 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		err := db.PingContext(pingCtx)
		cancel()
		if err == nil {
			return db
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSimpleSelectOne(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()

	db := openDB(t, addr)
	defer db.Close()

	rows, err := db.Query("SELECT 1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if len(cols) != 1 || cols[0] != "?column?" {
		t.Fatalf("unexpected columns: %#v", cols)
	}

	if !rows.Next() {
		t.Fatalf("expected one row, got none: %v", rows.Err())
	}
	var got int
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	if rows.Next() {
		t.Fatalf("expected exactly one row")
	}
}

func TestSimpleSelectArithmetic(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()

	db := openDB(t, addr)
	defer db.Close()

	var got int
	if err := db.QueryRow("SELECT 1 + 1").Scan(&got); err != nil {
		t.Fatalf("queryrow: %v", err)
	}
	if got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestUnsupportedQueryReturnsError(t *testing.T) {
	addr, stop := startTestServer(t)
	defer stop()

	db := openDB(t, addr)
	defer db.Close()

	_, err := db.Query("SELECT col FROM users")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "local execution") && !strings.Contains(err.Error(), "literal") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Connection should remain usable for a follow-up query — the
	// handler must always emit ReadyForQuery after the ErrorResponse.
	var got int
	if err := db.QueryRow("SELECT 7").Scan(&got); err != nil {
		t.Fatalf("recovery query failed: %v", err)
	}
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}
