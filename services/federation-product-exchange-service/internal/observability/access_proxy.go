package observability

import (
	"errors"
	"fmt"
	"time"

	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/models"
)

// ErrAccessGrantExpired is returned by ValidateAccess when the grant's
// expires_at is in the past. Mirrors the Rust string "access grant has
// expired".
var ErrAccessGrantExpired = errors.New("access grant has expired")

// ValidateAccess ports `domain::access_proxy::validate_access` 1:1.
//
// Returns ErrAccessGrantExpired when the grant has expired and a formatted
// "purpose '<purpose>' is not allowed by this contract" error when the
// requested purpose is not in the grant's allowed_purposes set.
func ValidateAccess(grant *models.AccessGrant, purpose string) error {
	if grant.ExpiresAt.Before(time.Now().UTC()) {
		return ErrAccessGrantExpired
	}

	for _, candidate := range grant.AllowedPurposes {
		if candidate == purpose {
			return nil
		}
	}
	return fmt.Errorf("purpose '%s' is not allowed by this contract", purpose)
}

// ResolveLimit ports `domain::access_proxy::resolve_limit` 1:1.
//
// Picks the requested limit when present, falling back to the grant's
// max_rows_per_query, then clamps to [1, grant_limit]. Negative grant limits
// fall back to 1000 to match Rust's `usize::try_from(grant.max_rows_per_query)
// .unwrap_or(1000)`.
func ResolveLimit(grant *models.AccessGrant, requested *int) int {
	grantLimit := 1000
	if grant.MaxRowsPerQuery >= 0 {
		grantLimit = int(grant.MaxRowsPerQuery)
	}

	limit := grantLimit
	if requested != nil {
		limit = *requested
	}
	if limit > grantLimit {
		limit = grantLimit
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}
