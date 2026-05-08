// Package migrations exposes the SQL migration files as an embedded
// filesystem so the pipeline-build-service binary can apply them at
// startup without needing a writable migrations directory at runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
