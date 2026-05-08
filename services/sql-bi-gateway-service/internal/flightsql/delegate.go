package flightsql

import (
	"context"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// delegateQuery forwards sql to a remote Flight SQL endpoint and
// collects every Arrow record batch it returns. Used to fan out
// queries to sql-warehousing-service (Iceberg shared compute pool)
// and to the Vespa / Postgres / Trino Flight SQL fronts. Mirrors
// `delegate_to_remote` in flight_sql.rs.
func delegateQuery(ctx context.Context, endpoint, sql string) ([]arrow.RecordBatch, *arrow.Schema, error) {
	client, err := dialFlightSQL(ctx, endpoint)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	info, err := client.Execute(ctx, sql)
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "remote execute failed: %s", err)
	}

	var (
		batches []arrow.RecordBatch
		schema  *arrow.Schema
	)
	for _, ep := range info.GetEndpoint() {
		tkt := ep.GetTicket()
		if tkt == nil {
			return nil, nil, status.Error(codes.Internal, "remote FlightInfo missing ticket")
		}
		reader, err := client.DoGet(ctx, tkt)
		if err != nil {
			return nil, nil, status.Errorf(codes.Internal, "remote do_get failed: %s", err)
		}
		if schema == nil {
			schema = reader.Schema()
		}
		for reader.Next() {
			rec := reader.RecordBatch()
			rec.Retain()
			batches = append(batches, rec)
		}
		if err := reader.Err(); err != nil {
			reader.Release()
			return nil, nil, status.Errorf(codes.Internal, "remote stream decode failed: %s", err)
		}
		reader.Release()
	}
	return batches, schema, nil
}

// delegateUpdate forwards a non-returning DDL/DML statement.
func delegateUpdate(ctx context.Context, endpoint, sql string) error {
	client, err := dialFlightSQL(ctx, endpoint)
	if err != nil {
		return err
	}
	defer client.Close()
	if _, err := client.ExecuteUpdate(ctx, sql); err != nil {
		return status.Errorf(codes.Internal, "remote execute_update failed: %s", err)
	}
	return nil
}

// dialFlightSQL connects to a remote Flight SQL endpoint and returns
// a flightsql.Client. `endpoint` accepts either a `host:port` pair or
// a URL (we strip the scheme + trailing slash so the gRPC dialer
// sees a `host:port` target — same shape `Endpoint::from_shared`
// accepts in tonic).
func dialFlightSQL(ctx context.Context, endpoint string) (*flightsql.Client, error) {
	target := normaliseEndpoint(endpoint)
	client, err := flightsql.NewClientCtx(ctx, target, nil, nil,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "backend %q unreachable: %s", endpoint, err)
	}
	return client, nil
}

// normaliseEndpoint strips the scheme + trailing slash so gRPC's
// dialer sees a `host:port` target.
func normaliseEndpoint(endpoint string) string {
	t := strings.TrimSpace(endpoint)
	for _, prefix := range []string{"http://", "https://", "grpc://", "grpc+tcp://"} {
		t = strings.TrimPrefix(t, prefix)
	}
	t = strings.TrimRight(t, "/")
	return t
}
