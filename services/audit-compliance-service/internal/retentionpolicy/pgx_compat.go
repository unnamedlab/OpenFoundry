package retentionpolicy

import "github.com/jackc/pgx/v5"

// pgxErrNoRows aliases pgx.ErrNoRows so the preview file can stay
// pgx-import-free in the helper layer above.
var pgxErrNoRows = pgx.ErrNoRows
