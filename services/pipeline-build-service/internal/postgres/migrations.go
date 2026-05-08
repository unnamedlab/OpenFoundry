package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/migrations"
)

var migrationsFS = migrations.FS

// RunMigrations executes the Rust-origin pipeline-build-service SQL migrations
// once per database. Filenames are recorded verbatim so duplicate timestamp
// prefixes remain distinct and ordering stays byte-for-byte compatible with the
// Rust migrations directory. Files are embedded into the binary so the
// migration loader works inside distroless containers.
func RunMigrations(ctx context.Context, db DB) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS pipeline_build_schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir(".")
	if err != nil {
		return err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		var applied bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pipeline_build_schema_migrations WHERE filename=$1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		raw, err := migrationsFS.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, string(raw)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO pipeline_build_schema_migrations (filename) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}
