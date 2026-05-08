package analyticallogic_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	analyticallogic "github.com/openfoundry/openfoundry-go/libs/analytical-logic"
)

// Smoke-checks the error types and small surface that doesn't need a
// live Postgres. The real round-trip exercise happens in the consumer
// service's integration tests where a Postgres testcontainer is
// available — same boundary the Rust crate's mod tests draw.
func TestErrNotFoundCarriesID(t *testing.T) {
	t.Parallel()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	got := &analyticallogic.ErrNotFound{ID: id}
	if !strings.Contains(got.Error(), id.String()) {
		t.Fatalf("ErrNotFound.Error() = %q, missing id %s", got.Error(), id)
	}
}

func TestErrDatabaseUnwraps(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection refused")
	err := &analyticallogic.ErrDatabase{Cause: cause}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is should match the wrapped cause")
	}
	if !strings.Contains(err.Error(), "analytical-logic repo: connection refused") {
		t.Fatalf("Error() = %q, missing prefix + cause", err.Error())
	}
}

func TestRepoConstructorStoresPool(t *testing.T) {
	t.Parallel()
	// nil pool is fine here — we only check the repo accessor wires
	// through. Any actual call on the repo would panic, mirroring the
	// Rust constructor smoke test.
	repo := analyticallogic.NewRepo(nil)
	if repo.Pool() != nil {
		t.Fatal("Pool() should return the same nil that was passed in")
	}
}
