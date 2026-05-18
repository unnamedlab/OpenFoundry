package repo

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migrations/*.sql in lexicographic order.
// Each file is goose-style (`-- +goose Up` / `-- +goose Down`); only the
// Up section is executed. Statements are idempotent (CREATE … IF NOT
// EXISTS), so re-running on an already-migrated database is safe.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		upSQL := extractGooseUp(string(body))
		if strings.TrimSpace(upSQL) == "" {
			continue
		}
		if _, err := pool.Exec(ctx, upSQL); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// extractGooseUp returns everything between `-- +goose Up` and
// `-- +goose Down`. When no markers are present the full body is
// returned so files authored as plain SQL still apply.
func extractGooseUp(body string) string {
	lines := strings.Split(body, "\n")
	var (
		out      []string
		inUp     bool
		hasMark  bool
		downSeen bool
	)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "-- +goose Up"):
			hasMark = true
			inUp = true
			continue
		case strings.HasPrefix(trimmed, "-- +goose Down"):
			hasMark = true
			inUp = false
			downSeen = true
			continue
		}
		if downSeen {
			continue
		}
		if inUp || !hasMark {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
