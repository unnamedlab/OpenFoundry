// Integration test: round-trip `SELECT 1` through the embedded
// Flight SQL server backed by [queryengine.QueryContext], with
// `allow_anonymous = true` so no JWT is required.
//
// Mirrors services/sql-bi-gateway-service/tests/flight_sql_round_trip.rs:
// open a Flight SQL channel, call Execute, fetch the resulting Arrow
// stream, and assert a single row with value 1.

package flightsql

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
)

func TestFlightSQLSelectOneRoundTrip(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	addr := ln.Addr().String()

	cfg := &config.Config{
		Host:           "127.0.0.1",
		AllowAnonymous: true,
		JWTSecret:      "test-secret",
	}
	svc := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- svc.Serve(ctx, ln)
	}()
	t.Cleanup(func() { _ = svc.Stop() })

	// Wait for the gRPC server to come up.
	var client *flightsql.Client
	for attempt := 0; attempt < 50; attempt++ {
		c, err := flightsql.NewClientCtx(ctx, addr, nil, nil,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			client = c
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("could not connect to in-process Flight SQL server at %s", addr)
	}
	defer client.Close()

	info, err := client.Execute(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("execute SELECT 1: %v", err)
	}
	if len(info.GetEndpoint()) == 0 {
		t.Fatalf("FlightInfo must contain at least one endpoint")
	}

	totalRows := 0
	sawValueOne := false
	for _, ep := range info.GetEndpoint() {
		tkt := ep.GetTicket()
		if tkt == nil {
			t.Fatal("endpoint must carry a ticket")
		}
		reader, err := client.DoGet(ctx, tkt)
		if err != nil {
			t.Fatalf("do_get: %v", err)
		}
		for reader.Next() {
			rec := reader.RecordBatch()
			totalRows += int(rec.NumRows())
			if col, ok := rec.Column(0).(*array.Int64); ok {
				for i := 0; i < col.Len(); i++ {
					if !col.IsNull(i) && col.Value(i) == 1 {
						sawValueOne = true
					}
				}
			}
		}
		if err := reader.Err(); err != nil {
			reader.Release()
			t.Fatalf("decode batch: %v", err)
		}
		reader.Release()
	}

	if totalRows != 1 {
		t.Fatalf("SELECT 1 must produce exactly one row, got %d", totalRows)
	}
	if !sawValueOne {
		t.Fatalf("the single row must contain the value 1")
	}

	// Trigger graceful shutdown.
	if err := svc.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("server did not shut down in time")
	}
	_ = arrow.Null // keep arrow import live for future expansions
}

// TestFlightSQLCatalogSentinel verifies the catalog response carries
// the GatewayCatalog string — the same advertisement BI clients see
// when they expand the connection navigator.
func TestFlightSQLCatalogSentinel(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	addr := ln.Addr().String()

	cfg := &config.Config{
		Host:           "127.0.0.1",
		AllowAnonymous: true,
		JWTSecret:      "test-secret",
	}
	svc := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = svc.Serve(ctx, ln) }()
	t.Cleanup(func() { _ = svc.Stop() })

	var client *flightsql.Client
	for attempt := 0; attempt < 50; attempt++ {
		c, err := flightsql.NewClientCtx(ctx, addr, nil, nil,
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			client = c
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("connect failed")
	}
	defer client.Close()

	info, err := client.GetCatalogs(ctx)
	if err != nil {
		t.Fatalf("get catalogs: %v", err)
	}
	if len(info.GetEndpoint()) == 0 {
		t.Fatal("expected at least one endpoint")
	}
	reader, err := client.DoGet(ctx, info.GetEndpoint()[0].GetTicket())
	if err != nil {
		t.Fatalf("do_get: %v", err)
	}
	defer reader.Release()
	if !reader.Next() {
		t.Fatal("expected at least one batch")
	}
	rec := reader.RecordBatch()
	col := rec.Column(0).(*array.String)
	if col.Value(0) != GatewayCatalog {
		t.Fatalf("want %s, got %s", GatewayCatalog, col.Value(0))
	}
}
