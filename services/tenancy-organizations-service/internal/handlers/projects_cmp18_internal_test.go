package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCMP18ResolveProjectPropagationPreventsReenable(t *testing.T) {
	t.Parallel()
	disabledAt := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	enabled, gotDisabledAt, status, message := resolveProjectPropagationPatch(
		false,
		&disabledAt,
		true,
		true,
		time.Date(2026, 5, 17, 13, 0, 0, 0, time.UTC),
	)

	assert.False(t, enabled)
	require.NotNil(t, gotDisabledAt)
	assert.Equal(t, disabledAt, *gotDisabledAt)
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, message, "cannot be re-enabled")
}

func TestCMP18ResolveProjectPropagationDisablingStampsOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	enabled, disabledAt, status, message := resolveProjectPropagationPatch(
		true,
		nil,
		false,
		true,
		now,
	)

	assert.False(t, enabled)
	require.NotNil(t, disabledAt)
	assert.Equal(t, now, *disabledAt)
	assert.Zero(t, status)
	assert.Empty(t, message)

	againEnabled, againDisabledAt, status, message := resolveProjectPropagationPatch(
		false,
		disabledAt,
		false,
		true,
		now.Add(time.Hour),
	)

	assert.False(t, againEnabled)
	require.NotNil(t, againDisabledAt)
	assert.Equal(t, now, *againDisabledAt)
	assert.Zero(t, status)
	assert.Empty(t, message)
}

func TestCMP18DecodeStringSliceJSONNormalizesMarkings(t *testing.T) {
	t.Parallel()

	got := decodeStringSliceJSON([]byte(`["ri.marking.main.marking.pii"," ri.marking.main.marking.pii ","","ri.marking.main.marking.fin"]`))

	assert.Equal(t, []string{
		"ri.marking.main.marking.pii",
		"ri.marking.main.marking.fin",
	}, got)
}

func TestCMP19SameStringSliceNormalizesMarkings(t *testing.T) {
	t.Parallel()

	assert.True(t, sameStringSlice(
		[]string{"ri.marking.main.marking.pii", " ri.marking.main.marking.pii "},
		[]string{"ri.marking.main.marking.pii"},
	))
	assert.False(t, sameStringSlice(
		[]string{"ri.marking.main.marking.pii"},
		[]string{"ri.marking.main.marking.fin"},
	))
}

func TestCMP19AppendPropagationDependentCapsAuditList(t *testing.T) {
	t.Parallel()
	target := propagationTarget{
		Kind:         "folder",
		ID:           uuid.MustParse("018f2f1c-aaaa-7bbb-8ccc-000000000019"),
		RID:          "ri.compass.main.folder.018f2f1c-aaaa-7bbb-8ccc-000000000019",
		Relationship: "view_requirements_child_folder",
	}
	deps := make([]audittrail.AffectedDependent, 0, viewReqAuditDependentCap)
	for i := 0; i < viewReqAuditDependentCap+3; i++ {
		deps = appendPropagationDependent(deps, target)
	}
	require.Len(t, deps, viewReqAuditDependentCap)
	assert.Equal(t, "view_requirements_updated", deps[0].Action)
}
