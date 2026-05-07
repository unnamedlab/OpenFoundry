package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExpiredItem is one row returned by the retention reaper. Surfaced
// to the caller so byte cleanup + audit emission stay decoupled from
// the SQL itself. Mirrors the Rust struct.
type ExpiredItem struct {
	RID         string
	MediaSetRID string
	Branch      string
	SHA256      string
	SizeBytes   int64
}

// ReapDue runs one global reaper pass. The "current effective"
// expiration is computed via JOIN with the parent media set so a
// PATCH that reduced retention is honoured even if the per-item
// snapshot still reflects the old value (mirrors the Rust contract).
//
// The UPDATE … RETURNING returns one row per item that was just
// expired so the caller can drop the bytes + bump the metric.
func ReapDue(ctx context.Context, pool *pgxpool.Pool) ([]ExpiredItem, error) {
	rows, err := pool.Query(ctx,
		`UPDATE media_items i
		    SET deleted_at = NOW()
		   FROM media_sets s
		  WHERE i.media_set_rid = s.rid
		    AND i.deleted_at IS NULL
		    AND s.retention_seconds > 0
		    AND i.created_at + s.retention_seconds * interval '1 second' < NOW()
		 RETURNING i.rid, i.media_set_rid, i.branch, i.sha256, i.size_bytes`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExpiredItem, 0)
	for rows.Next() {
		v := ExpiredItem{}
		if err := rows.Scan(&v.RID, &v.MediaSetRID, &v.Branch, &v.SHA256, &v.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReapMediaSet runs one reaper pass restricted to a single media set.
// Called synchronously from the PATCH handler so a retention
// reduction shows up in subsequent reads without waiting for the next
// periodic pass.
func ReapMediaSet(ctx context.Context, pool *pgxpool.Pool, mediaSetRID string) ([]ExpiredItem, error) {
	rows, err := pool.Query(ctx,
		`UPDATE media_items i
		    SET deleted_at = NOW()
		   FROM media_sets s
		  WHERE i.media_set_rid = s.rid
		    AND i.media_set_rid = $1
		    AND i.deleted_at IS NULL
		    AND s.retention_seconds > 0
		    AND i.created_at + s.retention_seconds * interval '1 second' < NOW()
		 RETURNING i.rid, i.media_set_rid, i.branch, i.sha256, i.size_bytes`,
		mediaSetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExpiredItem, 0)
	for rows.Next() {
		v := ExpiredItem{}
		if err := rows.Scan(&v.RID, &v.MediaSetRID, &v.Branch, &v.SHA256, &v.SizeBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
